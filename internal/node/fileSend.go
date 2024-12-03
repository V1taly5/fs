package node

import (
	"bytes"
	"crypto/ed25519"
	"encoding/gob"
	"fmt"
	"fs/internal/cover"
	"fs/internal/peers"
	"fs/internal/util/logger/sl"
	"io"
	"log/slog"
	"os"
)

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
