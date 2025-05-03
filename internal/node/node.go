package node

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"fs/internal/cover"
	"fs/internal/crypto"
	"fs/internal/db"
	"fs/internal/indexer"
	"fs/internal/indexer/hasher"
	"fs/internal/indexer/storage"
	"fs/internal/peers"
	"fs/internal/util/logger/sl"
	"fs/internal/watcher"
	fsv1 "fs/proto/gen/go"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"
)

const basePath = "/home/vito/Source/Documents"

type Node struct {
	log      *slog.Logger
	Port     int
	Name     string
	Peers    *peers.Peers
	PubKey   ed25519.PublicKey
	PrivKey  ed25519.PrivateKey
	handlers map[string]func(peer *peers.Peer, cover *cover.Cover)
	Broker   chan *cover.Cover

	BlockStorage *storage.FileBlockStorage
	IndexDB      *db.IndexDB
	Indexer      *indexer.Indexer
	Watcher      *watcher.FileWatcher

	fileTransfers      map[string]*FileTransfer // Мапа для отслеживания передач файлов
	fileTransfersMutex sync.Mutex               // Мьютекс для защиты доступа к fileTransfers

	BlocksDir string // Директория для хранения блоков данных
}

func NewNode(name string, port int, log *slog.Logger) *Node {
	publicKey, privateKey := crypto.LoadKey(name)

	// db init
	dbCondig := db.Config{
		Path:       "db",
		FileMode:   0,
		Options:    nil,
		Serializer: nil,
	}
	db, err := db.NewIndexDB(dbCondig)
	if err != nil {
		log.Error("db is not started")
	}

	// blockStorage init
	blocksDir := ".blocks"
	blockStorage, err := storage.NewFileBlockStorage(blocksDir)
	if err != nil {
		log.Error("blockStorage is not init")
	}

	// hasher init
	hasher := hasher.NewSHA256Hasher(indexer.DefaultBlockSize, blockStorage)

	// indexer init
	indexerConfig := indexer.IndexerConfig{
		Database:     db,
		BlockStorage: blockStorage,
		RootDir:      "/home/vito/Source/projects/fs/testFolder",
		Hasher:       hasher,
		Logger:       nil,
	}

	indexer := indexer.NewIndexer(indexerConfig)

	err = os.MkdirAll(blocksDir, 0755)
	if err != nil {
		fmt.Println(err)
	}
	node := &Node{
		log:           log,
		Port:          port,
		Name:          name,
		Peers:         peers.NewPeers(),
		PubKey:        publicKey,
		PrivKey:       privateKey,
		handlers:      make(map[string]func(peer *peers.Peer, cover *cover.Cover)),
		fileTransfers: make(map[string]*FileTransfer, 0),

		IndexDB:      db,
		Indexer:      indexer,
		BlockStorage: blockStorage,
		BlocksDir:    blocksDir,
	}

	node.handlers["HAND"] = node.onHand
	node.handlers["MESS"] = node.onMess
	node.handlers["FILE"] = node.HandleFileMessage
	node.handlers[CmdIndexExchange] = node.handleIndexExchange
	node.handlers[CmdBlockRequest] = node.handleBlockRequest
	node.handlers[CmdBlockResponse] = node.handleBlockResponse

	// node.handlers["META"] = node.handleMetadataRequest
	// node.handlers["DATA"] = node.handleDataRequest
	// node.handlers["NOTF"] = node.handleNotification
	return node
}

// rename
func (n *Node) SendName(peer *peers.Peer) {
	const op = "name.SendName"
	log := n.log.With(slog.String("op", op))

	ephemPubKey, epemPrivKey := crypto.CreatePairEphemeralKey()

	handShake := peers.HandShake{
		Name:     n.Name,
		PubKey:   hex.EncodeToString(n.PubKey),
		EphemKey: hex.EncodeToString(ephemPubKey.Bytes()),
	}.ToJson()

	log.Info(hex.EncodeToString(n.PubKey))

	peer.SharedKey.Update(nil, epemPrivKey.Bytes())

	sign := ed25519.Sign(n.PrivKey, handShake)

	cover := cover.NewSignedCover("HAND", n.PubKey, make([]byte, 32), sign, handShake)
	cover.Send(peer)
}

func (n Node) SendMessage(peer *peers.Peer, msg string) {
	const op = "node.SendMessage"
	log := n.log.With(slog.String("op", op))

	if peer.SharedKey.Secret == nil {
		log.Error("Can't send message")
	}

	//Encript message
	cover := cover.NewSignedCover("MESS", n.PubKey, peer.PubKey, ed25519.Sign(n.PrivKey, []byte(msg)), []byte(msg))
	cover.Send(peer)
}

func (n *Node) UnregisterPeer(peer *peers.Peer) {
	const op = "node.UnregisterPeer"
	log := n.log.With(slog.String("op", op))

	n.Peers.Remove(peer)
	log.Info("UnRegister peer", slog.String("peer name", peer.Name))
}

func (n *Node) RegisterPeer(peer *peers.Peer) *peers.Peer {
	const op = "node.RegisterPeer"
	log := n.log.With(slog.String("op", op))

	if reflect.DeepEqual(peer.PubKey, n.PubKey) {
		return nil
	}

	n.Peers.Put(peer)

	log.Info("Register new peer", slog.Int(peer.Name, len(n.Peers.Peers)))

	return peer
}

// ListenPeer Начало прослушивания соединения с пиром
func (n Node) ListenPeer(ctx context.Context, peer *peers.Peer) {
	// ctx := context.Background()
	readWriter := bufio.NewReadWriter(bufio.NewReader(*peer.Conn), bufio.NewWriter(*peer.Conn))
	n.HandleNode(ctx, readWriter, peer)
}

// // New func
// func (n Node) SendFile(ctx context.Context, peer *Peer, filePath string) error {
// 	const op = "node.SendFile"
// 	log := n.log.With(slog.String("op", op))

// 	file, err := os.Open(filePath)
// 	if err != nil {
// 		log.Error("Failed to open file", slog.String("filePath", filePath), sl.Err(err))
// 		return err
// 	}
// 	defer file.Close()

// 	fileInfo, err := file.Stat()
// 	if err != nil {
// 		log.Error("Failed to stat file", slog.String("filePath", filePath), sl.Err(err))
// 		return err
// 	}

// 	fileName := filepath.Base(filePath)
// 	fileHeader := NewCover("FILE", []byte(fileName))
// 	fileHeader.Length = uint16(fileInfo.Size())

// 	// Send file header first
// 	err = fileHeader.Send(peer)
// 	if err != nil {
// 		log.Error("Failed to send file header", sl.Err(err))
// 		return err
// 	}

// 	log.Info("Sending file...", slog.String("fileName", fileName), slog.Any("address", (*peer.Conn).RemoteAddr()))

// 	buffer := make([]byte, 1024)
// 	for {
// 		select {
// 		case <-ctx.Done():
// 			log.Info("Context canceled, stopping file transfer")
// 			return ctx.Err()
// 		default:
// 			nBytes, err := file.Read(buffer)
// 			if err != nil {
// 				if err == io.EOF {
// 					log.Info("File transfer completed", slog.String("fileName", fileName))
// 					return nil
// 				}
// 				log.Error("Error reading file", sl.Err(err))
// 				return err
// 			}

// 			_, err = (*peer.Conn).Write(buffer[:nBytes])
// 			if err != nil {
// 				log.Error("Error sending file chunk", sl.Err(err))
// 				return err
// 			}
// 		}
// 	}
// }

func (n *Node) compareIndexes(peer *peers.Peer, remoteFiles []*indexer.FileIndex) {
	const op = "node.compareIndexes"

	log := n.log.With(slog.String("op", op))
	// Получаем локальные индексы файлов

	log.Debug("inside node.compareIndexes")
	// log.Debug("remoteFiles:", remoteFiles)

	localFiles, err := n.IndexDB.GetAllFileIndexes()
	if err != nil {
		log.Info("Failed to get local file indexes: ", sl.Err(err))
		return
	}
	// log.Debug("localFiles:", localFiles)

	// Создаем мапы для быстрого доступа к индексам файлов по пути
	localIndexMap := make(map[string]*indexer.FileIndex)
	for _, fi := range localFiles {
		localIndexMap[fi.Path] = fi
	}

	remoteIndexMap := make(map[string]*indexer.FileIndex)
	for _, fi := range remoteFiles {
		remoteIndexMap[fi.Path] = fi
	}

	// Список запросов на недостающие или измененные файлы/блоки
	var missingBlocks []BlockRequest

	// Сравниваем файлы из удаленного индекса
	for path, remoteFile := range remoteIndexMap {
		localFile, exists := localIndexMap[path]

		if !exists {
			// Файл отсутствует локально, запрашиваем все блоки
			for _, block := range remoteFile.Blocks {
				missingBlocks = append(missingBlocks, BlockRequest{
					FilePath:   path,
					BlockIndex: block.Index,
				})
			}
		} else {
			// // Файл существует локально, сравниваем хеши
			// if localFile.FileHash != remoteFi.FileHash {
			// 	// Хеши файлов не совпадают, сравниваем блоки
			// 	remoteBlocksMap := make(map[int][32]byte)
			// 	for _, block := range remoteFi.Blocks {
			// 		remoteBlocksMap[block.Index] = block.Hash
			// 	}

			// 	localBlocksMap := make(map[int][32]byte)
			// 	for _, block := range localFi.Blocks {
			// 		localBlocksMap[block.Index] = block.Hash
			// 	}

			// 	// Определяем недостающие или измененные блоки
			// 	for index, remoteHash := range remoteBlocksMap {
			// 		localHash, exists := localBlocksMap[index]
			// 		if !exists || localHash != remoteHash {
			// 			// Блок отсутствует или изменен, добавляем в список запроса
			// 			missingBlocks = append(missingBlocks, BlockRequest{
			// 				FilePath:   path,
			// 				BlockIndex: index,
			// 			})
			// 		}
			// 	}
			// }

			if localFile.FileHash != remoteFile.FileHash {
				// Проверяем на конфликт
				if localFile.LastSyncedVersionID != "" && localFile.Versions[len(localFile.Versions)-1].VersionID != localFile.LastSyncedVersionID {
					// Конфликт
					n.handleConflict(peer, localFile, remoteFile)
				} else {
					// Файл изменен на пиру, обновляем его локально
					missing := compareBlocks(localFile, remoteFile)
					missingBlocks = append(missingBlocks, missing...)
				}
			}
		}
	}
	fmt.Println("Пропущенные блоки ", missingBlocks)
	// Запрашиваем недостающие блоки у пира
	if len(missingBlocks) > 0 {
		n.requestMissingBlocks(peer, missingBlocks)
	}
}

func compareBlocks(localFile *indexer.FileIndex, remoteFile *indexer.FileIndex) []BlockRequest {
	localBlocks := make(map[int][32]byte)
	for _, block := range localFile.Blocks {
		localBlocks[block.Index] = block.Hash
	}

	var missingBlocks []BlockRequest
	for _, remoteBlock := range remoteFile.Blocks {
		localHash, exists := localBlocks[remoteBlock.Index]
		if !exists || localHash != remoteBlock.Hash {
			// Блок отсутствует локально или отличается
			missingBlocks = append(missingBlocks, BlockRequest{
				FilePath:   remoteFile.Path,
				BlockIndex: remoteBlock.Index,
			})
		}
	}
	return missingBlocks
}

// compareBlocks compares local and remote blocks to find what's missing locally
func compareBlocksNew(localFile, remoteFile *indexer.FileIndex) []int {
	localBlockMap := make(map[int][32]byte)
	for _, block := range localFile.Blocks {
		localBlockMap[block.Index] = block.Hash
	}

	var missingIndices []int
	for _, remoteBlock := range remoteFile.Blocks {
		localHash, exists := localBlockMap[remoteBlock.Index]
		if !exists || localHash != remoteBlock.Hash {
			missingIndices = append(missingIndices, remoteBlock.Index)
		}
	}

	return missingIndices
}

func compareBlocksPtr(localFile *indexer.FileIndex, remoteFile *indexer.FileIndex) []*BlockRequest {
	localBlocks := make(map[int][32]byte)
	for _, block := range localFile.Blocks {
		localBlocks[block.Index] = block.Hash
	}

	var missingBlocks []*BlockRequest
	for _, remoteBlock := range remoteFile.Blocks {
		localHash, exists := localBlocks[remoteBlock.Index]
		if !exists || localHash != remoteBlock.Hash {
			// Блок отсутствует локально или отличается
			br := &BlockRequest{
				FilePath:   remoteFile.Path,
				BlockIndex: remoteBlock.Index,
			}
			missingBlocks = append(missingBlocks, br)
		}
	}
	return missingBlocks
}

func (n *Node) readBlock(filePath string, blockIndex int) ([]byte, error) {
	const BlockSize = 128 * 1024 // Размер блока: 128 КБ

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Вычисляем смещение для чтения блока
	offset := int64(blockIndex) * BlockSize

	// Переходим к нужному месту в файле
	_, err = file.Seek(offset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	// Читаем блок данных
	buf := make([]byte, BlockSize)
	nRead, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return nil, err
	}

	if nRead == 0 {
		return nil, fmt.Errorf("no data read from file: %s at block index: %d", filePath, blockIndex)
	}

	return buf[:nRead], nil
}

func (n *Node) writeBlock(filePath string, blockIndex int, data []byte) error {
	const BlockSize = 128 * 1024 // Размер блока: 128 КБ

	// Убедимся, что директория для файла существует
	dir := filepath.Dir(filePath)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	// Открываем файл для чтения и записи, создаем, если не существует
	file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// Вычисляем смещение для записи блока
	offset := int64(blockIndex) * BlockSize

	// Переходим к нужному месту в файле
	_, err = file.Seek(offset, io.SeekStart)
	if err != nil {
		return err
	}

	// Записываем данные блока
	nWritten, err := file.Write(data)
	if err != nil {
		return err
	}

	if nWritten != len(data) {
		return fmt.Errorf("written bytes %d does not match data length %d", nWritten, len(data))
	}

	return nil
}

func (n *Node) HandleNode(ctx context.Context, rw *bufio.ReadWriter, peer *peers.Peer) {
	const op = "node.HandleNode"
	log := n.log.With(slog.String("op", op))

	defer func() {
		if peer != nil {
			n.UnregisterPeer(peer)
			log.Info("Peer unregistered successfully",
				slog.Any("address", (*peer.Conn).RemoteAddr()),
			)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			log.Info("Context canceeld, shutting down connection handler")
			return
		default:
			cover, err := cover.ReadCover(rw.Reader)
			if err != nil {
				if err != io.EOF {
					log.Error("Error on read Cover", sl.Err(err))
				}
				log.Error("Disconected peer", sl.Err(err))
				return
			}

			if ed25519.Verify(cover.From, cover.Message, cover.Sign) {
				log.Info("Signed cover!")
			}

			log.Info("LISTENER: receive cover from",
				slog.Any("address", (*peer.Conn).RemoteAddr()),
			)

			handler, found := n.handlers[string(cover.Cmd)]
			if !found {
				log.Info("LISTENER: UNHSNDLED NODE MESSAGE",
					slog.String("CMD", string(cover.Cmd)),
					slog.String("ID", string(cover.Id)),
					slog.String("Message", string(cover.Message)),
				)
				continue
			}
			handler(peer, cover)
		}
	}
}

func (n *Node) CompareIndexes(remoteFiles []*indexer.FileIndex) []*fsv1.BlockRequest {
	const op = "node.compareIndexes"
	log := n.log.With(slog.String("op", op))
	log.Debug("inside node.compareIndexes")

	localFiles, err := n.IndexDB.GetAllFileIndexes()
	if err != nil {
		log.Error("Failed to get local file indexes: ", sl.Err(err))
		return nil
	}

	localIndexMap := make(map[string]*indexer.FileIndex)
	for _, fi := range localFiles {
		localIndexMap[fi.Path] = fi
	}

	remoteIndexMap := make(map[string]*indexer.FileIndex)
	for _, fi := range remoteFiles {
		remoteIndexMap[fi.Path] = fi
	}

	var blockRequests []*fsv1.BlockRequest

	for path, remoteFile := range remoteIndexMap {
		localFile, exists := localIndexMap[path]

		if !exists {
			// file doesn't exist locally, request all blocks
			log.Debug("file doesn't exist locally, requesting all blocks",
				slog.String("path", path),
			)
			blockRequests = append(blockRequests, createBlockRequestsForFile(path, remoteFile.Blocks)...)
		} else if localFile.FileHash != remoteFile.FileHash {
			// files are different, check for conflicts
			log.Debug("file hash mismatch",
				slog.String("path", path),
				slog.String("local", fmt.Sprintf("%x", localFile.FileHash)),
				slog.String("remote", fmt.Sprintf("%x", remoteFile.FileHash)),
			)

			if n.shouldHandleAsConflict(localFile, remoteFile) {
				// handle conflict case
				conflictRequest := n.handleConflictNew(localFile, remoteFile)
				if len(conflictRequest) > 0 {
					blockRequests = append(blockRequests, conflictRequest...)
				}
			} else {
				// no conflict, just get missing blocks
				missingBlocks := compareBlocksNew(localFile, remoteFile)
				if len(missingBlocks) > 0 {
					log.Debug("requesting missing blocks",
						slog.String("path", path),
						slog.Int("count", len(missingBlocks)),
					)
					fsv1Missing := convertToFsv1BlockRequests(path, missingBlocks)
					blockRequests = append(blockRequests, fsv1Missing...)
				}
			}
		} else {
			log.Debug("file is up to date", slog.String("path", path))
		}
	}

	// Check for locally deleted files that still exist remotely
	for path, localFile := range localIndexMap {
		if localFile.Deleted {
			if remoteFile, exists := remoteIndexMap[path]; exists && !remoteFile.Deleted {
				// handle deleted file conflict
				log.Debug("conflict: file deleted locally but exists remotely", slog.String("path", path))
			}
		}
	}

	if len(blockRequests) > 0 {
		log.Info("found blocks to request", slog.Int("count", len(blockRequests)))
		return blockRequests
	}

	log.Debug("no blocks to request")
	return nil
}

// createBlockRequestsForFile creates block requests for all blocks in a file
func createBlockRequestsForFile(path string, blocks []indexer.BlockHash) []*fsv1.BlockRequest {
	// create a request for each block
	var result []*fsv1.BlockRequest

	for _, block := range blocks {
		request := &fsv1.BlockRequest{
			FileId:       path,
			BlockIndexes: uint32(block.Index),
		}
		result = append(result, request)
	}

	return result
}

// shouldHandleAsConflict determines if we should handle this as a conflict case
// A conflict occurs when both local and remote have diverged from their common ancestor
func (n *Node) shouldHandleAsConflict(localFile, remoteFile *indexer.FileIndex) bool {
	// If we have no versions, we can't detect conflicts
	if len(localFile.Versions) == 0 || len(remoteFile.Versions) == 0 {
		return false
	}

	// If local file has been modified since last sync
	if localFile.LastSyncedVersionID != "" &&
		localFile.LastSyncedVersionID != localFile.Versions[len(localFile.Versions)-1].VersionID {

		// Find the common ancestor version
		var commonAncestorFound bool
		for _, localVersion := range localFile.Versions {
			if localVersion.VersionID == localFile.LastSyncedVersionID {
				commonAncestorFound = true
				break
			}
		}

		// If we found the common ancestor and remote has also diverged
		if commonAncestorFound &&
			remoteFile.LastSyncedVersionID != remoteFile.Versions[len(remoteFile.Versions)-1].VersionID {
			return true
		}
	}

	return false
}

// handleConflict handles file conflicts by implementing a conflict resolution strategy
// Returns any block requests needed after conflict resolution
func (n *Node) handleConflictNew(localFile, remoteFile *indexer.FileIndex) []*fsv1.BlockRequest {
	const op = "node.handleConflict"
	log := n.log.With(slog.String("op", op), slog.String("path", localFile.Path))
	log.Info("handling file conflict")

	// if remote file is newer, get its blocks
	if remoteFile.ModTime.After(localFile.ModTime) {
		log.Info("resolving conflict: remote file is newer",
			slog.Time("remote", remoteFile.ModTime),
			slog.Time("local", localFile.ModTime),
		)

		// create conflict file path with timestamp
		conflictPath := localFile.Path + ".conflict_" + time.Now().Format("20060102_150405")

		// rename local file to conflict file
		err := os.Rename(localFile.Path, conflictPath)
		if err != nil {
			log.Error("failed to create conflict file", sl.Err(err))
			// continue anyway to sync the remote version
		} else {
			log.Info("created conflict file", slog.String("conflict_path", conflictPath))
		}

		// request all blocks from the remote file
		return createBlockRequestsForFile(localFile.Path, remoteFile.Blocks)
	} else {
		log.Info("resolving conflict: keeping local file (newer)",
			slog.Time("local", localFile.ModTime),
			slog.Time("remote", remoteFile.ModTime),
		)

		return nil
	}
}

func ConvertToFsv1BlockRequests(requests []BlockRequest) ([]*fsv1.BlockRequest, error) {
	if len(requests) == 0 {
		return nil, fmt.Errorf("пустой список запросов")
	}

	// группируем BlockIndex по FilePath
	grouped := make(map[string][]int)
	for _, req := range requests {
		grouped[req.FilePath] = append(grouped[req.FilePath], req.BlockIndex)
	}

	// Создаём срез fsv1.BlockRequest
	var fsv1Requests []*fsv1.BlockRequest
	for filePath, indexes := range grouped {
		for _, index := range indexes {
			fsv1Req := &fsv1.BlockRequest{
				FileId:       filePath,
				BlockIndexes: uint32(index),
			}
			fsv1Requests = append(fsv1Requests, fsv1Req)
		}
	}

	return fsv1Requests, nil
}

// convertToFsv1BlockRequests converts a list of block indices to fsv1.BlockRequest objects
func convertToFsv1BlockRequests(path string, blockIndices []int) []*fsv1.BlockRequest {
	var result []*fsv1.BlockRequest

	for _, index := range blockIndices {
		request := &fsv1.BlockRequest{
			FileId:       path,
			BlockIndexes: uint32(index),
		}
		result = append(result, request)
	}

	return result
}
