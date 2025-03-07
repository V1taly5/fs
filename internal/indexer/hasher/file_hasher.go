package hasher

import (
	"crypto/sha256"
	"fmt"
	"fs/internal/indexer"
	"io"
	"os"
)

type SHA256Hasher struct {
	blockSize int
	storage   BlockStorage
}

func NewSHA256Hasher(blockSize int, storage BlockStorage) *SHA256Hasher {
	return &SHA256Hasher{
		blockSize: blockSize,
		storage:   storage,
	}
}

func (h *SHA256Hasher) HashFile(path string) ([]indexer.BlockHash, [32]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var blocks []indexer.BlockHash
	hasher := sha256.New()
	buf := make([]byte, h.blockSize)
	index := 0

	for {
		nRead, err := file.Read(buf)
		if err != nil && err != io.EOF {
			return nil, [32]byte{}, fmt.Errorf("failed to read file: %w", err)
		}
		if nRead == 0 {
			break
		}

		data := buf[:nRead]
		blockHash := sha256.Sum256(data)

		blocks = append(blocks, indexer.BlockHash{
			Index: index,
			Hash:  blockHash,
			Data:  append([]byte(nil), data...), // Create a copy of the data
		})

		hasher.Write(data)
		index++
	}

	var fileHash [32]byte
	copy(fileHash[:], hasher.Sum(nil))

	return blocks, fileHash, nil
}

func (h *SHA256Hasher) CalculateFileHash(blocks []indexer.BlockHash) ([32]byte, error) {
	hasher := sha256.New()

	for _, block := range blocks {
		data, err := h.storage.LoadBlock(block.Hash)
		if err != nil {
			return [32]byte{}, fmt.Errorf("failed to load block %x: %w", block.Hash, err)
		}
		hasher.Write(data)
	}

	var fileHash [32]byte
	copy(fileHash[:], hasher.Sum(nil))
	return fileHash, nil
}
