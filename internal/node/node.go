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
	"fs/internal/fileindex"
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

// const basePath = "/home/vito/Source/Documents"

type Node struct {
	log      *slog.Logger
	Port     int
	Name     string
	Peers    *peers.Peers
	PubKey   ed25519.PublicKey
	PrivKey  ed25519.PrivateKey
	handlers map[string]func(peer *peers.Peer, cover *cover.Cover)
	Broker   chan *cover.Cover

	Watcher *watcher.FileWatcher
	Indexer *fileindex.Indexer

	fileTransfers      map[string]*FileTransfer // Мапа для отслеживания передач файлов
	fileTransfersMutex sync.Mutex               // Мьютекс для защиты доступа к fileTransfers
}

func NewNode(name string, port int, log *slog.Logger) *Node {
	publicKey, privateKey := crypto.LoadKey(name)

	db, err := db.NewIndexDB("db")
	if err != nil {
		log.Error("db is not started")
	}
	indexer := fileindex.NewIndexer(db)
	node := &Node{
		log:           log,
		Port:          port,
		Name:          name,
		Peers:         peers.NewPeers(),
		PubKey:        publicKey,
		PrivKey:       privateKey,
		handlers:      make(map[string]func(peer *peers.Peer, cover *cover.Cover)),
		fileTransfers: make(map[string]*FileTransfer, 0),
		Indexer:       indexer,
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

func (n *Node) AddWathcer(watcher *watcher.FileWatcher) {
	n.Watcher = watcher
}

type IndexerInterface interface {
	GetAllFileIndexes() ([]*fileindex.FileIndex, error)
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

func (n *Node) SendMessage(peer *peers.Peer, msg string) {
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

	log.Info("Register new peer", slog.Int(peer.Name, len(n.Peers.GetPeers())))

	return peer
}

// ListenPeer Начало прослушивания соединения с пиром
func (n *Node) ListenPeer(ctx context.Context, peer *peers.Peer) {
	// ctx := context.Background()
	readWriter := bufio.NewReadWriter(bufio.NewReader(*peer.Conn), bufio.NewWriter(*peer.Conn))
	n.HandleNode(ctx, readWriter, peer)
}

// --------------------------- -------------------------- --------------------------

// сравнивает локальный индекс файлов с индексом полученным от пира
// определяет какие файлы или блоки нужно запросить у пира для синхронизации
func (n *Node) compareIndexes(peer *peers.Peer, remoteFiles []*fileindex.FileIndex) {
	const op = "node.compareIndexes"

	log := n.log.With(slog.String("op", op))
	// Получаем локальные индексы файлов

	log.Debug("inside node.compareIndexes")
	localFiles, err := n.Indexer.GetAllFileIndexes()
	if err != nil {
		log.Info("Failed to get local file indexes: ", sl.Err(err))
		return
	}

	// Создаем мапы для быстрого доступа к индексам файлов по пути
	localIndexMap := make(map[string]*fileindex.FileIndex)
	for _, fi := range localFiles {
		localIndexMap[fi.Path] = fi
	}

	remoteIndexMap := make(map[string]*fileindex.FileIndex)
	for _, fi := range remoteFiles {
		remoteIndexMap[fi.Path] = fi
	}

	// Список запросов на недостающие или измененные файлы/блоки
	var missingBlocks []BlockRequest
	fmt.Println("Пропущенные блоки ", missingBlocks)

	// Сравниваем файлы из удаленного индекса
	for path, remoteFi := range remoteIndexMap {
		localFi, exists := localIndexMap[path]

		if !exists {
			// Файл отсутствует локально, запрашиваем все блоки
			for _, block := range remoteFi.Blocks {
				missingBlocks = append(missingBlocks, BlockRequest{
					FilePath:   path,
					BlockIndex: block.Index,
				})
			}
		} else {
			// Файл существует локально, сравниваем хеши
			if localFi.FileHash != remoteFi.FileHash {
				// Хеши файлов не совпадают, сравниваем блоки
				remoteBlocksMap := make(map[int][32]byte)
				for _, block := range remoteFi.Blocks {
					remoteBlocksMap[block.Index] = block.Hash
				}

				localBlocksMap := make(map[int][32]byte)
				for _, block := range localFi.Blocks {
					localBlocksMap[block.Index] = block.Hash
				}

				// Определяем недостающие или измененные блоки
				for index, remoteHash := range remoteBlocksMap {
					localHash, exists := localBlocksMap[index]
					if !exists || localHash != remoteHash {
						// Блок отсутствует или изменен, добавляем в список запроса
						missingBlocks = append(missingBlocks, BlockRequest{
							FilePath:   path,
							BlockIndex: index,
						})
					}
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

// читает указанный блок из файла и возвращает его данные
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

// записывает полученные данные блока в файл по указанному индексу
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
