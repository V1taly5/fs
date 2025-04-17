package connectionmanager

import (
	"context"
	"fmt"
	"fs/internal/node"
	fsv1 "fs/proto/gen/go"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"
)

const DefaultMessageProcessTimeout = 30 * time.Second

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

func (mp *MessageProcessor) processMessage(ctx context.Context, conn Connection, msg *fsv1.Message) {
	var msgType string
	switch msg.Payload.(type) {
	case *fsv1.Message_Ping:
		msgType = "ping"
	case *fsv1.Message_Pong:
		msgType = "pong"
	default:
		mp.log.Error("Unknown message type",
			slog.String("sender", msg.SenderId),
			slog.Uint64("message_id", msg.MassageId))
		return
	}

	mp.handlersMu.RLock()
	handler, exists := mp.handlers[msgType]
	mp.handlersMu.RUnlock()

	if !exists {
		mp.log.Warn("No handler registered for message type",
			slog.String("message_type", msgType),
			slog.String("sender", msg.SenderId),
			slog.Uint64("message_id", msg.MassageId))
		return
	}
	if err := handler(ctx, conn, msg); err != nil {
		mp.log.Error("Error processing message",
			slog.String("message_type", msgType),
			slog.String("sender", msg.SenderId),
			slog.Uint64("message_id", msg.MassageId),
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

	log.Info("Starting message processing")

	msgChan := conn.MessageChannel()

	for {
		select {
		case msg, ok := <-msgChan:
			if !ok {
				log.Info("Connection message channel closed")
				return
			}

			// Отдельный контекст для обработки сообщения
			msgCtx, cancel := context.WithTimeout(mp.ctx, DefaultMessageProcessTimeout)
			mp.processMessage(msgCtx, conn, msg)
			cancel()
		case <-mp.ctx.Done():
			log.Info("Stoping message processing (context canseled)")
			return
		}
	}
}

func (mp *MessageProcessor) Stop() {
	mp.cancel()
	mp.processDone.Wait()
	mp.log.Info("Message processor stopped")
}

func handlePing(ctx context.Context, conn Connection, msg *fsv1.Message) error {
	// Deserialize the incoming PingMSG
	ping := msg.GetPing()
	nodeID := msg.GetSenderId()
	fmt.Println("Пришло новое сообщение")
	// Prepare the PongMSG with the same sequence number
	pongMSG := &fsv1.Message{
		MassageId:  generateMessageID(),
		Timestamp:  uint64(time.Now().UnixNano()),
		SenderId:   "d0827ff980b5ae539ba0a9e70892f1a7aa83ba9a26a16a0102de0e4f4c791888",
		ReceiverId: nodeID,
		Payload: &fsv1.Message_Pong{
			Pong: &fsv1.PongMSG{
				SeqNum:        ping.SeqNum,
				RoundTripTime: 0,
			},
		},
	}

	if err := validateUTF8Fields(pongMSG); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	if err := conn.SendMessage(ctx, pongMSG); err != nil {
		return fmt.Errorf("failed to send PongMSG: %w", err)
	}

	return nil
}

func generateMessageID() uint64 {
	return uint64(time.Now().UnixNano())
}

func validateUTF8Fields(msg *fsv1.Message) error {
	if !utf8.ValidString(msg.SenderId) {
		return fmt.Errorf("invalid UTF-8 in SenderId")
	}
	if !utf8.ValidString(msg.ReceiverId) {
		return fmt.Errorf("invalid UTF-8 in ReceiverId")
	}
	return nil
}
