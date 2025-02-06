package hasher

import (
	"crypto/sha256"
	"fs/internal/indexer"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockBlockStorage struct {
	blocks map[[32]byte][]byte
}

func newMockStorage() *mockBlockStorage {
	return &mockBlockStorage{
		blocks: make(map[[32]byte][]byte),
	}
}

func (m *mockBlockStorage) SaveBlock(hash [32]byte, data []byte) error {
	m.blocks[hash] = data
	return nil
}

func (m *mockBlockStorage) LoadBlock(hash [32]byte) ([]byte, error) {
	data, ok := m.blocks[hash]
	if !ok {
		return nil, indexer.ErrBlockNotFound
	}
	return data, nil
}

func TestSHA256Hasher_HashFile(t *testing.T) {
	content := []byte("test content for hashing")
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	err := os.WriteFile(tmpFile, content, 0644)
	require.NoError(t, err)

	storage := newMockStorage()
	hasher := NewSHA256Hasher(8, storage)

	blocks, fileHash, err := hasher.HashFile(tmpFile)
	require.NoError(t, err)

	expectedBlocks := (len(content) + 7) / 8
	assert.Equal(t, expectedBlocks, len(blocks))

	expectedFileHash := sha256.Sum256(content)
	assert.Equal(t, expectedFileHash, fileHash)

	offset := 0
	for i, block := range blocks {
		assert.Equal(t, i, block.Index)
		end := offset + 8
		if end > len(content) {
			end = len(content)
		}
		expectedBlockHash := sha256.Sum256(content[offset:end])
		assert.Equal(t, expectedBlockHash, block.Hash)
		assert.Equal(t, content[offset:end], block.Data)
		offset += 8
	}
}

func TestSHA256Hasher_CalculateFileHash(t *testing.T) {
	storage := newMockStorage()
	hasher := NewSHA256Hasher(8, storage)

	content := []byte("test content for hash calculation")
	var blocks []indexer.BlockHash

	for i := 0; i < len(content); i += 8 {
		end := i + 8
		if end > len(content) {
			end = len(content)
		}
		blockData := content[i:end]
		blockHash := sha256.Sum256(blockData)
		blocks = append(blocks, indexer.BlockHash{
			Index: i / 8,
			Hash:  blockHash,
			Data:  blockData,
		})
		storage.SaveBlock(blockHash, blockData)
	}

	fileHash, err := hasher.CalculateFileHash(blocks)
	require.NoError(t, err)

	expectedFileHash := sha256.Sum256(content)
	assert.Equal(t, expectedFileHash, fileHash)
}

func TestSHA256Hasher_ErrorCases(t *testing.T) {
	storage := newMockStorage()
	hasher := NewSHA256Hasher(8, storage)

	_, _, err := hasher.HashFile("non-existent-file")
	assert.Error(t, err)

	blocks := []indexer.BlockHash{{
		Index: 0,
		Hash:  [32]byte{1},
		Data:  []byte("test"),
	}}
	_, err = hasher.CalculateFileHash(blocks)
	assert.Error(t, err)
}
