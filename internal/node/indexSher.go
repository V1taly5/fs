package node

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"fs/internal/cover"
	"fs/internal/peers"
	"log/slog"
)

const (
	CmdIndexExchange = "IDEX" // Команда для обмена индексами
	CmdBlockRequest  = "BREQ" // Запрос блока
	CmdBlockResponse = "BRES" // Ответ с блоком
)

type IndexMessage struct {
	Files []*FileIndex
}

type BlockRequest struct {
	FilePath   string
	BlockIndex int
}

type BlockResponse struct {
	FilePath   string
	BlockIndex int
	Data       []byte
}

func (n *Node) SendIndex(peer *peers.Peer) error {
	// Получаем список файлов из базы данных
	files, err := n.indexDB.GetAllFileIndexes()
	if err != nil {
		return err
	}

	indexMsg := IndexMessage{Files: files}
	data, err := json.Marshal(indexMsg)
	if err != nil {
		return err
	}

	cover := cover.NewSignedCover(CmdIndexExchange, n.PubKey, peer.PubKey, ed25519.Sign(n.PrivKey, data), data)
	// err = cover.Send(peer)
	return cover.Send(peer)
}

func (n *Node) requestMissingBlocks(peer *peers.Peer, missingBlocks []BlockRequest) error {
	for _, req := range missingBlocks {
		data, err := json.Marshal(req)
		if err != nil {
			return err
		}
		cover := cover.NewSignedCover(CmdBlockRequest, n.PubKey, peer.PubKey, ed25519.Sign(n.PrivKey, data), data)
		// cover := NewCover(CmdBlockRequest, data)
		cover.Send(peer)
	}
	return nil
}

func (n *Node) handleBlockRequest(peer *peers.Peer, covers *cover.Cover) {
	var req BlockRequest
	err := json.Unmarshal(covers.Message, &req)
	if err != nil {
		n.log.Error("Failed to unmarshal block request", "error", err)
		return
	}

	// Читаем запрошенный блок из файла
	blockData, err := n.readBlock(req.FilePath, req.BlockIndex)
	if err != nil {
		n.log.Error("Failed to read block", "error", err)
		return
	}

	resp := BlockResponse{
		FilePath:   req.FilePath,
		BlockIndex: req.BlockIndex,
		Data:       blockData,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		n.log.Error("Failed to marshal block response", "error", err)
		return
	}
	responseCover := cover.NewSignedCover(CmdBlockResponse, n.PubKey, peer.PubKey, ed25519.Sign(n.PrivKey, data), data)
	// responseCover := NewCover(CmdBlockResponse, data)
	responseCover.Send(peer)
}

func (n *Node) handleBlockResponse(peer *peers.Peer, cover *cover.Cover) {
	var resp BlockResponse
	err := json.Unmarshal(cover.Message, &resp)
	if err != nil {
		n.log.Error("Failed to unmarshal block response", "error", err)
		return
	}

	// Сохраняем полученный блок в файл
	err = n.writeBlock(resp.FilePath, resp.BlockIndex, resp.Data)
	if err != nil {
		n.log.Error("Failed to write block", "error", err)
		return
	}
}

func (n *Node) handleIndexExchange(peer *peers.Peer, cover *cover.Cover) {
	const op = "node.handleIndexExchange"
	log := n.log.With(slog.String("op", op))
	log.Debug("inside handleIndexExchange")
	var indexMsg IndexMessage
	err := json.Unmarshal(cover.Message, &indexMsg)
	if err != nil {
		log.Error("Failed to unmarshal index message", "error", err)
		return
	}
	fmt.Println(indexMsg.Files)
	// Сравниваем индексы и определяем недостающие блоки
	n.compareIndexes(peer, indexMsg.Files)
}
