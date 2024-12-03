package indexer

import (
	"crypto/sha256"
	"encoding/gob"
	"io"
	"log"
	"os"
	"time"
)

func init() {
	gob.Register(FileIndex{})
	gob.Register(BlockHash{})
}

// IndexDatabase определяет интерфейс для работы с индексами файлов.
type IndexDatabase interface {
	UpdateFileIndex(fi *FileIndex) error
	GetFileIndex(path string) (*FileIndex, error)
	GetAllFileIndexes() ([]*FileIndex, error)
	Close() error
}

type Indexer struct {
	indexDB IndexDatabase
}

func NewIndexer(db IndexDatabase) *Indexer {
	return &Indexer{
		indexDB: db,
	}
}

type FileIndex struct {
	Path     string
	Size     int64
	ModTime  time.Time
	Blocks   []BlockHash
	FileHash [32]byte // SHA-256 хеш всего файла
}

type BlockHash struct {
	Index int
	Hash  [32]byte // SHA-256 хеш блока
}

func (i *Indexer) IndexFile(path string) {
	fileInfo, err := os.Stat(path)
	if os.IsNotExist(err) {
		log.Printf("File does not exist: %s", path)
		return
	} else if err != nil {
		log.Printf("Failed to stat file: %v", err)
		return
	}

	fileIndex := &FileIndex{
		Path:    path,
		Size:    fileInfo.Size(),
		ModTime: fileInfo.ModTime(),
	}

	// Разбиваем файл на блоки и вычисляем хеши
	blocks, fileHash, err := hashFile(path)
	if err != nil {
		log.Printf("Failed to hash file: %v", err)
		return
	}

	fileIndex.Blocks = blocks
	fileIndex.FileHash = fileHash

	// Обновляем индекс в базе данных
	err = i.indexDB.UpdateFileIndex(fileIndex)
	if err != nil {
		log.Printf("Failed to update file index: %v", err)
		return
	}
	// return n.indexDB.UpdateFileIndex(fileIndex)
	log.Printf("Indexed file: %s", path)
}

func hashFile(path string) ([]BlockHash, [32]byte, error) {
	const BlockSize = 128 * 1024 // Размер блока: 128 КБ

	file, err := os.Open(path)
	if err != nil {
		return nil, [32]byte{}, err
	}
	defer file.Close()

	var blocks []BlockHash
	hasher := sha256.New()

	buf := make([]byte, BlockSize)
	index := 0

	for {
		nRead, err := file.Read(buf)
		if err != nil && err != io.EOF {
			return nil, [32]byte{}, err
		}
		if nRead == 0 {
			break
		}

		data := buf[:nRead]

		// Вычисляем хеш блока
		blockHash := sha256.Sum256(data)

		// Добавляем хеш блока в список
		blocks = append(blocks, BlockHash{
			Index: index,
			Hash:  blockHash,
		})

		// Обновляем общий хеш файла
		hasher.Write(data)

		index++
	}

	// Вычисляем общий хеш файла
	var fileHash [32]byte
	copy(fileHash[:], hasher.Sum(nil))

	return blocks, fileHash, nil
}
