package node

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"fs/internal/cover"
	"fs/internal/indexer"
	"fs/internal/peers"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const (
	CmdIndexExchange = "IDEX" // Команда для обмена индексами
	CmdBlockRequest  = "BREQ" // Запрос блока
	CmdBlockResponse = "BRES" // Ответ с блоком
)

type IndexMessage struct {
	Files []*indexer.FileIndex
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
	files, err := n.IndexDB.GetAllFileIndexes()
	if err != nil {
		return err
	}

	indexMsg := IndexMessage{Files: files}
	fmt.Println("indexMsg.Files: ", indexMsg.Files)
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

	// Читаем запрошенный блок из хранилища
	fi, err := n.IndexDB.GetFileIndex(req.FilePath)
	if err != nil {
		log.Printf("Failed to get file index: %v", err)
		return
	}

	var blockHash [32]byte
	for _, block := range fi.Blocks {
		if block.Index == req.BlockIndex {
			blockHash = block.Hash
			break
		}
	}

	// Читаем запрошенный блок из файла
	blockData, err := n.BlockStorage.LoadBlock(blockHash)
	// blockData, err := indexer.LoadBlock(blockHash)
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
	n.log.Debug("Data:", data)
	responseCover.Send(peer)
}

func (n *Node) handleBlockResponse(peer *peers.Peer, cover *cover.Cover) {
	n.log.Debug("inside handleBlockResponse")

	var resp BlockResponse
	err := json.Unmarshal(cover.Message, &resp)
	if err != nil {
		n.log.Error("Failed to unmarshal block response", "error", err)
		return
	}
	n.log.Debug("Block Resp:", resp)

	// Сохраняем полученный блок
	blockHash := sha256.Sum256(resp.Data)

	n.BlockStorage.SaveBlock(blockHash, resp.Data)
	// err = indexer.SaveBlock(blockHash, resp.Data)
	if err != nil {
		log.Printf("Failed to save block: %v", err)
		return
	}

	// Обновляем индекс фsайла
	n.Indexer.UpdateFileIndexAfterBlockWrite(resp.FilePath, resp.BlockIndex, blockHash)

	fi, err := n.IndexDB.GetFileIndex(resp.FilePath)
	if err != nil {
		n.log.Error("Failed to get file index", "error", err)
		return
	}

	// Восстанавливаем файл из блоков
	n.Indexer.RestoreFileFromBlocks(resp.FilePath, fi.Blocks)
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
	fmt.Println("cover.Message: ", cover.Message)
	fmt.Println("indexMsg.Files: ", indexMsg.Files)
	// Сравниваем индексы и определяем недостающие блоки
	n.compareIndexes(peer, indexMsg.Files)
}

func (n *Node) handleConflict(peer *peers.Peer, localFile *indexer.FileIndex, remoteFile *indexer.FileIndex) {
	log.Printf("Conflict detected for file: %s", localFile.Path)
	var blocksDir = ".blocks"
	// Сохраняем локальную версию с новым именем
	conflictPath := fmt.Sprintf("%s_conflict_%d", localFile.Path, time.Now().Unix())
	err := os.Rename(localFile.Path, conflictPath)
	if err != nil {
		log.Printf("Failed to rename conflicting file: %v", err)
		return
	}

	// Удаляем локальные блоки, так как файл будет обновлен
	for _, block := range localFile.Blocks {
		blockPath := filepath.Join(blocksDir, fmt.Sprintf("%x", block.Hash))
		os.Remove(blockPath)
	}

	// Запрашиваем все блоки удаленного файла
	var missingBlocks []BlockRequest
	for _, block := range remoteFile.Blocks {
		missingBlocks = append(missingBlocks, BlockRequest{
			FilePath:   remoteFile.Path,
			BlockIndex: block.Index,
		})
	}
	n.requestMissingBlocks(peer, missingBlocks)

	// Обновляем LastSyncedVersionID
	localFile.LastSyncedVersionID = remoteFile.Versions[len(remoteFile.Versions)-1].VersionID

	// Сохраняем обновленный индекс
	err = n.IndexDB.UpdateFileIndex(localFile)
	if err != nil {
		log.Printf("Failed to update file index after conflict resolution: %v", err)
	}
}
