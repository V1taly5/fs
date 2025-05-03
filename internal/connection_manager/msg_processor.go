package connectionmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"fs/internal/indexer"
	"fs/internal/node"
	"fs/internal/util/logger/sl"
	fsv1 "fs/proto/gen/go"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"
)

// MessageType constants for different message types
const (
	MessageTypePing         = "ping"
	MessageTypePong         = "pong"
	MessageTypeIndexUpdate  = "indexes"
	MessageTypeBlockRequest = "BREQ"
	MessageTypeBlockResp    = "BRES"
)

// Default timeouts
const DefaultMessageProcessTimeout = 30 * time.Second

// ErrInvalidUTF8 represents an error when string contains invalid UTF-8 data
var ErrInvalidUTF8 = errors.New("invalid UTF-8 encoding")

// MsgHandler defines a function type for message handlers
type MsgHandler func(context.Context, Connection, *fsv1.Message) error

type MessageProcessor struct {
	node        *node.Node
	log         *slog.Logger
	handlers    map[string]MsgHandler
	handlersMu  sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	processDone sync.WaitGroup
}

func NewMessageProcessor(ctx context.Context, node *node.Node, log *slog.Logger) *MessageProcessor {
	ctx, cancel := context.WithCancel(ctx)

	mp := &MessageProcessor{
		node:     node,
		log:      log.With(slog.String("component", "message_processor")),
		handlers: make(map[string]MsgHandler),
		ctx:      ctx,
		cancel:   cancel,
	}

	return mp
}

func (mp *MessageProcessor) RegisterHandler(msgType string, handler MsgHandler) {
	mp.handlersMu.Lock()
	defer mp.handlersMu.Unlock()

	mp.log.Info("Registering message handler", slog.String("message_type", msgType))
	mp.handlers[msgType] = handler
}

// RegisterDefaultHandlers registers the default set of message handlers
func (mp *MessageProcessor) RegisterDefaultHandlers() {
	mp.RegisterHandler(MessageTypeIndexUpdate, mp.HandleIndexes)
	mp.RegisterHandler(MessageTypeBlockRequest, mp.HandleBlockRequest)
	mp.RegisterHandler(MessageTypeBlockResp, mp.HandleBlockResponse)
}

// getMessageType determines the type of the message
func getMessageType(msg *fsv1.Message) string {
	switch msg.Payload.(type) {
	case *fsv1.Message_Ping:
		return MessageTypePing
	case *fsv1.Message_Pong:
		return MessageTypePong
	case *fsv1.Message_IndexUpdate:
		return MessageTypeIndexUpdate
	case *fsv1.Message_BlockRequest:
		return MessageTypeBlockRequest
	case *fsv1.Message_BlockResponse:
		return MessageTypeBlockResp
	default:
		return ""
	}
}

func (mp *MessageProcessor) processMessage(ctx context.Context, conn Connection, msg *fsv1.Message) {
	op := "msg_processor.processMessage"
	log := mp.log.With(
		slog.String("op", op),
		slog.String("sender", msg.SenderId),
		slog.Uint64("message_id", msg.MassageId),
	)
	log.Debug("Processing message")
	msgType := getMessageType(msg)
	if msgType == "" {
		log.Error("Unknown message type")
		return
	}

	mp.handlersMu.RLock()
	handler, exists := mp.handlers[msgType]
	mp.handlersMu.RUnlock()

	if !exists {
		mp.log.Warn("No handler registered for message type",
			slog.String("message_type", msgType),
		)
		return
	}
	if err := handler(ctx, conn, msg); err != nil {
		mp.log.Error("Error processing message",
			slog.String("message_type", msgType),
			slog.String("reciver_id", msg.ReceiverId),
			slog.String("error", err.Error()))
	}
}

func (mp *MessageProcessor) ProcessConnectionMessages(conn Connection) {
	mp.processDone.Add(1)
	defer mp.processDone.Done()

	nodeID := conn.NodeID()
	log := mp.log.With(
		slog.String("node_id", nodeID),
		slog.String("address", conn.Address()),
	)

	log.Debug("Starting message processing")

	msgChan := conn.MessageChannel()

	for {
		select {
		case msg, ok := <-msgChan:
			if !ok {
				log.Info("Connection message channel closed")
				return
			}

			// Create a separate context for message processing
			msgCtx, cancel := context.WithTimeout(mp.ctx, DefaultMessageProcessTimeout)
			mp.processMessage(msgCtx, conn, msg)
			cancel()
		case <-mp.ctx.Done():
			log.Info("Stoping message processing (context canseled)")
			return
		}
	}
}

// Stop gracefully stops the message processor
func (mp *MessageProcessor) Stop() {
	mp.cancel()
	mp.processDone.Wait()
	mp.log.Info("Message processor stopped")
}

// HandlePing handles ping messages
func HandlePing(ctx context.Context, conn Connection, msg *fsv1.Message) error {
	ping := msg.GetPing()
	nodeID := msg.GetSenderId()

	pongMsg := &fsv1.Message{
		MassageId: generateMessageID(),
		Timestamp: uint64(time.Now().UnixNano()),
		// TODO
		// SenderId:   conn.SelfID(),
		ReceiverId: nodeID,
		Payload: &fsv1.Message_Pong{
			Pong: &fsv1.PongMSG{
				SeqNum:        ping.SeqNum,
				RoundTripTime: 0, // Could calculate actual RTT here
			},
		},
	}

	if err := ValidateUTF8Fields(pongMsg); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	if err := conn.SendMessage(ctx, pongMsg); err != nil {
		return fmt.Errorf("failed to send PongMSG: %w", err)
	}

	return nil
}

func (mp *MessageProcessor) HandleIndexes(ctx context.Context, conn Connection, msg *fsv1.Message) error {
	op := "msg_processor.HandleIndexes"
	log := mp.log.With(
		slog.String("op", op),
		slog.String("sender", msg.SenderId),
		slog.String("receiver_id", msg.ReceiverId),
	)

	fsv1Indexes := msg.GetIndexUpdate()
	log.Debug("Handling indexes update")

	indexes, err := ConvertIndexUpdateToFileIndexes(fsv1Indexes)
	if err != nil {
		return fmt.Errorf("failed to convert indexes: %w", err)
	}

	missingBlocks := mp.node.CompareIndexes(indexes)
	if len(missingBlocks) == 0 {
		log.Debug("No missing blocks to request")
		return nil
	}

	if err := mp.requestMissingBlocks(ctx, conn, msg.SenderId, missingBlocks); err != nil {
		return fmt.Errorf("failed to send block requests: %w", err)
	}

	return nil
}

// requestMissingBlocks sends requests for blocks that are missing
func (mp *MessageProcessor) requestMissingBlocks(ctx context.Context, conn Connection, receiverID string, missingBlocks []*fsv1.BlockRequest) error {
	op := "msg_processor.request_missing_blocks"
	log := mp.log.With(
		slog.String("op", op),
		slog.Int("block_count", len(missingBlocks)),
	)
	log.Debug("Requesting missing blocks")

	var errs []error

	// TODO: add SelfID to Conntction interface (conn.SelfID -> string)
	hashPubKey := sha256.Sum256(mp.node.PubKey)
	nodeId := hex.EncodeToString(hashPubKey[:])

	for _, blockReq := range missingBlocks {
		if !isValidUTF8(blockReq.FileId) {
			err := fmt.Errorf("%w in FiledId: %s", ErrInvalidUTF8, blockReq.FileId)
			log.Error("Invalid UTF-8 in FileId",
				slog.String("file_id", blockReq.FileId),
				sl.Err(err),
			)
			errs = append(errs, err)
			continue
		}

		responseMsg := &fsv1.Message{
			MassageId:  generateMessageID(),
			Timestamp:  uint64(time.Now().UnixNano()),
			SenderId:   nodeId, //conn.SelfId()
			ReceiverId: receiverID,
			Payload: &fsv1.Message_BlockRequest{
				BlockRequest: blockReq,
			},
		}

		if err := ValidateUTF8Fields(responseMsg); err != nil {
			log.Error("UTF-8 validation failed",
				sl.Err(err),
			)
			errs = append(errs, err)
			continue
		}

		if err := conn.SendMessage(ctx, responseMsg); err != nil {
			log.Error("Failed to send block request",
				slog.String("file_id", blockReq.FileId),
				slog.String("block_id", fmt.Sprintf("%d", blockReq.BlockIndexes)),
				sl.Err(err),
			)
			errs = append(errs, err)
			continue
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("encountered errors while sending block requests: %v", errs)
	}

	log.Debug("All missing blocks have been requested individually")
	return nil
}

// HandleBlockRequest processes block request messages
func (mp *MessageProcessor) HandleBlockRequest(ctx context.Context, conn Connection, msg *fsv1.Message) error {
	op := "msg_processor.handle_block_request"
	log := mp.log.With(
		slog.String("op", op),
	)

	blockRequest := msg.GetBlockRequest()

	// Get the requested file indexs
	fi, err := mp.node.IndexDB.GetFileIndex(blockRequest.FileId)
	if err != nil {
		log.Error("Failed to get file index",
			sl.Err(err),
		)
		return fmt.Errorf("failed to get file index: %w", err)
	}

	// Find the requested block
	var blockHash [32]byte
	found := false
	for _, block := range fi.Blocks {
		if block.Index == int(blockRequest.BlockIndexes) {
			blockHash = block.Hash
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("block with index %d not found in file %s", blockRequest.BlockIndexes, blockRequest.FileId)
	}

	// Read the requested block from storage
	blockData, err := mp.node.BlockStorage.LoadBlock(blockHash)
	if err != nil {
		log.Error("Failed to read block",
			sl.Err(err),
		)
		return fmt.Errorf("failed to read block: %w", err)
	}

	hashPubKey := sha256.Sum256(mp.node.PubKey)
	nodeId := hex.EncodeToString(hashPubKey[:])

	respMsg := &fsv1.Message{
		MassageId:  generateMessageID(),
		Timestamp:  uint64(time.Now().UnixNano()),
		SenderId:   nodeId,
		ReceiverId: msg.GetSenderId(),
		Payload: &fsv1.Message_BlockResponse{
			BlockResponse: &fsv1.BlockResponse{
				FileId: blockRequest.FileId,
				Blocks: &fsv1.Block{
					Index: blockRequest.BlockIndexes,
					Data:  blockData,
				},
			},
		},
	}

	log.Debug("Sending block response",
		slog.String("file_id", blockRequest.FileId),
		slog.Uint64("block_index", uint64(blockRequest.BlockIndexes)),
		slog.Int("data_size", len(blockData)))
	return conn.SendMessage(ctx, respMsg)
}

// HandleBlockResponse processes block response messages
func (mp *MessageProcessor) HandleBlockResponse(ctx context.Context, conn Connection, msg *fsv1.Message) error {
	op := "msg_processor.handle_block_response"
	log := mp.log.With(
		slog.String("op", op),
	)
	log.Debug("Processing block response")

	blockResponse := msg.GetBlockResponse()
	if blockResponse == nil || blockResponse.Blocks == nil {
		return errors.New("invalid block response: missing data")
	}

	// Calculate block hash and save the block
	blockHash := sha256.Sum256(blockResponse.Blocks.Data)
	if err := mp.node.BlockStorage.SaveBlock(blockHash, blockResponse.Blocks.Data); err != nil {
		log.Error("Failed to save block", sl.Err(err))
		return fmt.Errorf("failed to save block: %w", err)
	}

	err := mp.node.Indexer.UpdateFileIndexAfterBlockWrite(blockResponse.FileId, int(blockResponse.Blocks.Index), blockHash)
	if err != nil {
		log.Error("Failed to update file index", sl.Err(err))
		return fmt.Errorf("failed to update file index: %w", err)
	}

	// Get the updated file index
	fi, err := mp.node.IndexDB.GetFileIndex(blockResponse.FileId)
	if err != nil {
		log.Error("Failed to get file index", sl.Err(err))
		return fmt.Errorf("failed to get updated file index: %w", err)
	}

	// Restore file from blocks
	if err := mp.node.Indexer.RestoreFileFromBlocks(blockResponse.FileId, fi.Blocks); err != nil {
		log.Error("Failed to restore file from blocks", sl.Err(err))
		return fmt.Errorf("failed to restore file: %w", err)
	}

	log.Info("Successfully processed block and updated file",
		slog.String("file_id", blockResponse.FileId),
		slog.Int64("block_index", int64(blockResponse.Blocks.Index)),
	)

	return nil
}

// IsValidUTF8 checks if a string contains valid UTF-8 characters
func isValidUTF8(s string) bool {
	return utf8.ValidString(s)
}

// GenerateMessageID generates a unique message ID
func generateMessageID() uint64 {
	return uint64(time.Now().UnixNano())
}

// ValidateUTF8Fields validates that all string fields in the message have valid UTF-8 encoding
func ValidateUTF8Fields(msg *fsv1.Message) error {
	if !utf8.ValidString(msg.SenderId) {
		return fmt.Errorf("%w in SenderId", ErrInvalidUTF8)
	}
	if !utf8.ValidString(msg.ReceiverId) {
		return fmt.Errorf("%w in ReceiverId", ErrInvalidUTF8)
	}
	return nil
}

// ConvertFileInfoToFileIndex converts a protobuf FileInfo to a FileIndex
func ConvertFileInfoToFileIndex(fi *fsv1.FileInfo) (*indexer.FileIndex, error) {
	var fileHash [32]byte
	if len(fi.FileHash) != 32 {
		return nil, fmt.Errorf("invalid file hash length: expected 32, got %d", len(fi.FileHash))
	}
	copy(fileHash[:], fi.FileHash)

	blocks := make([]indexer.BlockHash, len(fi.Blocks))
	for i, b := range fi.Blocks {
		var hash [32]byte
		if len(b.Hash) != 32 {
			return nil, fmt.Errorf("invalid block hash length at index %d: expected 32, got %d", i, len(b.Hash))
		}
		copy(hash[:], b.Hash)
		blocks[i] = indexer.BlockHash{
			Index: int(b.Index),
			Hash:  hash,
		}
	}

	return &indexer.FileIndex{
		Path:                fi.Path,
		Size:                int64(fi.Size),
		ModTime:             time.Unix(int64(fi.ModifiedTime), 0),
		Blocks:              blocks,
		FileHash:            fileHash,
		Versions:            nil, // незаполнено
		LastSyncedVersionID: "",  // незаполнено
		Deleted:             fi.Deleted,
	}, nil
}

// ConvertIndexUpdateToFileIndexes converts a protobuf IndexUpdate to FileIndexes
func ConvertIndexUpdateToFileIndexes(update *fsv1.IndexUpdate) ([]*indexer.FileIndex, error) {
	var fileIndexes []*indexer.FileIndex
	for _, fi := range update.Files {
		fileIndex, err := ConvertFileInfoToFileIndex(fi)
		if err != nil {
			return nil, err
		}
		fileIndexes = append(fileIndexes, fileIndex)
	}
	return fileIndexes, nil
}
