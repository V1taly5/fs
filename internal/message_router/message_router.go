package messagerouter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	connectionmanager "fs/internal/connection_manager"
	"fs/internal/indexer"
	"fs/internal/node"
	fsv1 "fs/proto/gen/go"
	"log/slog"
	"time"
)

type MessageRouter struct {
	node    *node.Node
	indexer *indexer.Indexer
	connMgr *connectionmanager.ConnectionManager
	log     *slog.Logger
}

func NewMessageRouter(
	node *node.Node, indexer *indexer.Indexer, cm *connectionmanager.ConnectionManager, log *slog.Logger,
) *MessageRouter {
	return &MessageRouter{
		node:    node,
		indexer: indexer,
		connMgr: cm,
		log:     log,
	}
}

func (r *MessageRouter) sendMessage(ctx context.Context, nodeID string, msg *fsv1.Message) error {
	conn, ok := r.connMgr.GetActiveConnection(nodeID)
	if !ok {
		return fmt.Errorf("no active connection for nodeID %s", nodeID)
	}

	if !conn.IsActive() {
		return fmt.Errorf("connection to nodeID %s is inactive", nodeID)
	}

	return conn.SendMessage(ctx, msg)
}

func (r *MessageRouter) SendIndexUpdate(
	ctx context.Context,
	nodeID string,
	isFullUpdate bool,
	sequence uint64,
) error {

	hashPubKey := sha256.Sum224(r.node.PubKey)
	nodeId := hex.EncodeToString(hashPubKey[:])

	files, err := r.indexer.GetAllFileIndexes()
	if err != nil {
		return err
	}
	fsv1Files := ConvertToProtoFileIndexes(files)

	msg := &fsv1.Message{
		MassageId:  generateMessageID(),
		Timestamp:  uint64(time.Now().UnixNano()),
		SenderId:   nodeId,
		ReceiverId: nodeID,
		Payload: &fsv1.Message_IndexUpdate{
			IndexUpdate: &fsv1.IndexUpdate{
				Files:        fsv1Files,
				IsFullUpdate: isFullUpdate,
				Sequence:     sequence,
			},
		},
	}

	return r.sendMessage(ctx, nodeID, msg)
}

// Сейчас реализация отличается от прошлой, запрашивается
// информация об одном файле за одни раз (надо изменить обработчик)
func (r *MessageRouter) SendRequestMissingBlock(
	ctx context.Context,
	nodeID string,
	filePath string,
	blockIndexes []uint32,
) error {
	hashPubKey := sha256.Sum224(r.node.PubKey)
	nodeId := hex.EncodeToString(hashPubKey[:])
	msg := &fsv1.Message{
		MassageId:  generateMessageID(),
		Timestamp:  uint64(time.Now().UnixNano()),
		SenderId:   nodeId,
		ReceiverId: nodeID,
		Payload: &fsv1.Message_BlockRequest{
			BlockRequest: &fsv1.BlockRequest{
				FileId:       filePath,
				BlockIndexes: blockIndexes,
			},
		},
	}

	return r.sendMessage(ctx, nodeID, msg)
}

func (r *MessageRouter) SendResponseBlock(
	ctx context.Context,
	nodeID string,
	filePath string,
	blocks []*indexer.BlockHash,
) error {
	fsv1Blocks := ConvertToProtoBlocks(blocks)
	hashPubKey := sha256.Sum224(r.node.PubKey)
	nodeId := hex.EncodeToString(hashPubKey[:])
	msg := &fsv1.Message{
		MassageId:  generateMessageID(),
		Timestamp:  uint64(time.Now().UnixNano()),
		SenderId:   nodeId,
		ReceiverId: nodeID,
		Payload: &fsv1.Message_BlockResponse{
			BlockResponse: &fsv1.BlockResponse{
				FileId: filePath,
				Blocks: fsv1Blocks,
			},
		},
	}

	return r.sendMessage(ctx, nodeID, msg)
}

func generateMessageID() uint64 {
	return uint64(time.Now().UnixNano())
}

func ConvertToProtoFileIndexes(fileIndices []*indexer.FileIndex) []*fsv1.FileInfo {
	var protoFiles []*fsv1.FileInfo

	for _, fileIndex := range fileIndices {
		var protoBlocks []*fsv1.BlockInfo
		for _, block := range fileIndex.Blocks {
			protoBlocks = append(protoBlocks, &fsv1.BlockInfo{
				Index: uint32(block.Index),
				Hash:  block.Hash[:],
			})
		}

		protoFile := &fsv1.FileInfo{
			Path:         fileIndex.Path,
			Size:         uint64(fileIndex.Size),
			ModifiedTime: uint64(fileIndex.ModTime.Unix()),
			FileHash:     fileIndex.FileHash[:],
			Blocks:       protoBlocks,
			Deleted:      fileIndex.Deleted,
		}

		protoFiles = append(protoFiles, protoFile)
	}

	return protoFiles
}

func ConvertToProtoBlocks(blockHashes []*indexer.BlockHash) []*fsv1.Block {
	protoBlocks := make([]*fsv1.Block, len(blockHashes))

	for i, blockHash := range blockHashes {
		protoBlock := &fsv1.Block{
			Index: uint32(blockHash.Index),
			Data:  blockHash.Data,
			Hash:  blockHash.Hash[:],
		}
		protoBlocks[i] = protoBlock
	}

	return protoBlocks
}
