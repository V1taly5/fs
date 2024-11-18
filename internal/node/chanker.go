package node

import (
	"crypto/sha256"
	"io"
	"os"

	"github.com/restic/chunker"
)

const BlockSize = 128 * 1024 // 128 КБ

func splitFileIntoBlocks(path string) ([]BlockHash, [32]byte, error) {
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
		n, err := file.Read(buf)
		if err != nil && err != io.EOF {
			return nil, [32]byte{}, err
		}
		if n == 0 {
			break
		}

		blockData := buf[:n]
		blockHash := sha256.Sum256(blockData)
		blocks = append(blocks, BlockHash{Index: index, Hash: blockHash})

		hasher.Write(blockData)
		index++
	}

	fileHash := hasher.Sum(nil)
	var fileHashArray [32]byte
	copy(fileHashArray[:], fileHash)

	return blocks, fileHashArray, nil
}

func splitFileWithCDC(path string) ([]BlockHash, [32]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, [32]byte{}, err
	}
	defer file.Close()

	var blocks []BlockHash
	hasher := sha256.New()

	polynomial := chunker.Pol(0x3DA3358B4DC173) // Используем стандартный полином
	ch := chunker.New(file, polynomial)

	index := 0

	for {
		chunk, err := ch.Next(nil)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, [32]byte{}, err
		}

		blockHash := sha256.Sum256(chunk.Data)
		blocks = append(blocks, BlockHash{Index: index, Hash: blockHash})

		hasher.Write(chunk.Data)
		index++
	}

	fileHash := hasher.Sum(nil)
	var fileHashArray [32]byte
	copy(fileHashArray[:], fileHash)

	return blocks, fileHashArray, nil
}

func computeWeakHash(data []byte) uint32 {
	var hash uint32
	for _, b := range data {
		hash = (hash << 1) + uint32(b)
	}
	return hash
}
