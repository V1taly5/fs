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
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync"
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
