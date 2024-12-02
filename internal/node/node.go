package node

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"fs/internal/cover"
	"fs/internal/crypto"
	"fs/internal/peers"
	"fs/internal/util/logger/sl"
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

	indexDB *IndexDB
	watcher *FileWatcher

	fileTransfers      map[string]*FileTransfer // Мапа для отслеживания передач файлов
	fileTransfersMutex sync.Mutex               // Мьютекс для защиты доступа к fileTransfers
}

func NewNode(name string, port int, log *slog.Logger) *Node {
	publicKey, privateKey := crypto.LoadKey(name)

	db, err := NewIndexDB("db")
	if err != nil {
		log.Error("db is not started")
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
		indexDB:       db,
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

// Максимальный размер сообщения, который может быть отправлен
const MaxMessageSize = 65535

// Максимальный размер данных в одном куске файла
const MaxChunkDataSize = 60000

// Структура для представления куска файла
type FileChunk struct {
	FileName    string // Имя файла
	ChunkNumber uint32 // Номер текущего куска
	TotalChunks uint32 // Общее количество кусков
	ChunkData   []byte // Данные куска
}

// Функция для сериализации структуры FileChunk в байтовый массив
func serializeFileChunk(fc *FileChunk) ([]byte, error) {
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	err := encoder.Encode(fc)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Функция для отправки файла
func (n Node) SendFile(peer *peers.Peer, filePath string) {
	const op = "node.SendFile"
	log := n.log.With("op", op)
	// Открываем файл для чтения
	file, err := os.Open(filePath)
	if err != nil {
		return
	}
	defer file.Close()

	// Получаем информацию о файле
	fileInfo, err := file.Stat()
	if err != nil {
		return
	}

	fileName := fileInfo.Name()
	fileSize := fileInfo.Size()
	// Вычисляем общее количество кусков
	totalChunks := uint32((fileSize + MaxChunkDataSize - 1) / MaxChunkDataSize)

	buffer := make([]byte, MaxChunkDataSize)

	chunkNumber := uint32(0)
	for {
		// Читаем данные из файла в буфер
		bytesRead, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return
		}

		if bytesRead == 0 {
			break // Конец файла
		}

		// Создаем структуру FileChunk для текущего куска
		fileChunk := &FileChunk{
			FileName:    fileName,
			ChunkNumber: chunkNumber,
			TotalChunks: totalChunks,
			ChunkData:   buffer[:bytesRead],
		}

		// Сериализуем кусок файла в байты
		messageBytes, err := serializeFileChunk(fileChunk)
		if err != nil {
			return
		}

		fmt.Printf("Размер сериализованного сообщения: %d байт\n", len(messageBytes))

		// Проверяем, не превышает ли сообщение максимальный размер
		if len(messageBytes) > MaxMessageSize {
			err = fmt.Errorf("Размер сообщения %d превышает максимальный допустимый %d", len(messageBytes), MaxMessageSize)
			log.Info("Error", sl.Err(err))
		}

		// Создаем Cover с командой "FILE" и отправляем его
		// cover := NewCover("FILE", messageBytes)
		cover := cover.NewSignedCover("FILE", n.PubKey, peer.PubKey, ed25519.Sign(n.PrivKey, messageBytes), messageBytes)
		cover.Send(peer)

		chunkNumber++
	}

	return
}

// Структура для хранения информации о приеме файла
type FileTransfer struct {
	TotalChunks    uint32            // Общее количество кусков
	ReceivedChunks map[uint32][]byte // Полученные куски
}

// Функция для десериализации байтов в структуру FileChunk
func deserializeFileChunk(data []byte) (*FileChunk, error) {
	buf := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buf)
	var fc FileChunk
	err := decoder.Decode(&fc)
	if err != nil {
		return nil, err
	}
	return &fc, nil
}

// Обработчик для приема кусков файла
func (n *Node) HandleFileMessage(peer *peers.Peer, cover *cover.Cover) {
	// Десериализуем полученное сообщение в FileChunk
	fileChunk, err := deserializeFileChunk(cover.Message)
	if err != nil {
		n.log.Error("Не удалось десериализовать кусок файла", sl.Err(err))
		return
	}

	// Блокируем доступ к общей структуре fileTransfers
	n.fileTransfersMutex.Lock()
	defer n.fileTransfersMutex.Unlock()

	// Проверяем, есть ли уже запись о передаче этого файла
	ft, exists := n.fileTransfers[fileChunk.FileName]
	if !exists {
		// Если нет, создаем новую
		ft = &FileTransfer{
			TotalChunks:    fileChunk.TotalChunks,
			ReceivedChunks: make(map[uint32][]byte),
		}
		n.fileTransfers[fileChunk.FileName] = ft
	}

	// Сохраняем полученный кусок
	ft.ReceivedChunks[fileChunk.ChunkNumber] = fileChunk.ChunkData

	n.log.Info("Получен кусок файла",
		slog.String("FileName", fileChunk.FileName),
		slog.Int("ChunkNumber", int(fileChunk.ChunkNumber)),
		slog.Int("TotalChunks", int(fileChunk.TotalChunks)),
	)

	// Проверяем, все ли куски получены
	if uint32(len(ft.ReceivedChunks)) == ft.TotalChunks {
		n.log.Info("Все куски получены, собираем файл",
			slog.String("FileName", fileChunk.FileName),
		)
		// Собираем файл из полученных кусков
		err := assembleFile(fileChunk.FileName, ft)
		if err != nil {
			n.log.Error("Ошибка при сборке файла", sl.Err(err))
		} else {
			n.log.Info("Файл успешно собран",
				slog.String("FileName", fileChunk.FileName),
			)
		}
		// Удаляем запись о передаче файла
		delete(n.fileTransfers, fileChunk.FileName)
	}
}

// Функция для сборки файла из кусков
func assembleFile(fileName string, ft *FileTransfer) error {
	// fullaPath := fmt.Sprintf("%s/%s", basePath, fileName)
	// Создаем новый файл для записи
	file, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	// Пишем куски в файл в правильном порядке
	for i := uint32(0); i < ft.TotalChunks; i++ {
		chunkData, exists := ft.ReceivedChunks[i]
		if !exists {
			return fmt.Errorf("Отсутствует кусок %d", i)
		}
		_, err := file.Write(chunkData)
		if err != nil {
			return err
		}
	}

	return nil
}

func (n *Node) compareIndexes(peer *peers.Peer, remoteFiles []*FileIndex) {
	const op = "node.compareIndexes"

	log := n.log.With(slog.String("op", op))
	// Получаем локальные индексы файлов

	log.Debug("inside node.compareIndexes")
	localFiles, err := n.indexDB.GetAllFileIndexes()
	if err != nil {
		log.Info("Failed to get local file indexes: ", sl.Err(err))
		return
	}

	// Создаем мапы для быстрого доступа к индексам файлов по пути
	localIndexMap := make(map[string]*FileIndex)
	for _, fi := range localFiles {
		localIndexMap[fi.Path] = fi
	}

	remoteIndexMap := make(map[string]*FileIndex)
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

	// Запрашиваем недостающие блоки у пира
	if len(missingBlocks) > 0 {
		n.requestMissingBlocks(peer, missingBlocks)
	}
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
