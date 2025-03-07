package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"fs/internal/indexer"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileBlockStorage(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("NewFileBlockStorage", func(t *testing.T) {
		storage, err := NewFileBlockStorage(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, storage)

		// Test creating storage with invalid path
		storage, err = NewFileBlockStorage("/invalid/path/that/cannot/be/created")
		assert.Error(t, err)
		assert.Nil(t, storage)
	})

	t.Run("SaveBlock", func(t *testing.T) {
		storage, err := NewFileBlockStorage(tmpDir)
		require.NoError(t, err)

		data := []byte("test data")
		hash := sha256.Sum256(data)

		// Test saving new block
		err = storage.SaveBlock(hash, data)
		assert.NoError(t, err)

		// Verify file exists
		blockPath := filepath.Join(tmpDir, hex.EncodeToString(hash[:]))
		_, err = os.Stat(blockPath)
		assert.NoError(t, err)

		// Test saving same block again (idempotency)
		err = storage.SaveBlock(hash, data)
		assert.NoError(t, err)

		// Test saving block with empty data
		emptyHash := sha256.Sum256([]byte{})
		err = storage.SaveBlock(emptyHash, []byte{})
		assert.NoError(t, err)
	})

	t.Run("LoadBlock", func(t *testing.T) {
		storage, err := NewFileBlockStorage(tmpDir)
		require.NoError(t, err)

		data := []byte("test data for loading")
		hash := sha256.Sum256(data)

		// Save block first
		err = storage.SaveBlock(hash, data)
		require.NoError(t, err)

		// Test loading existing block
		loadedData, err := storage.LoadBlock(hash)
		assert.NoError(t, err)
		assert.Equal(t, data, loadedData)

		// Test loading non-existent block
		nonexistentHash := sha256.Sum256([]byte("nonexistent"))
		_, err = storage.LoadBlock(nonexistentHash)
		assert.ErrorIs(t, err, indexer.ErrBlockNotFound)

		// Test loading from corrupted file
		blockPath := filepath.Join(tmpDir, hex.EncodeToString(hash[:]))
		err = os.Chmod(blockPath, 0000)
		require.NoError(t, err)
		_, err = storage.LoadBlock(hash)
		assert.Error(t, err)
		_ = os.Chmod(blockPath, 0644)
	})

	t.Run("DeleteBlock", func(t *testing.T) {
		storage, err := NewFileBlockStorage(tmpDir)
		require.NoError(t, err)

		data := []byte("test data for deletion")
		hash := sha256.Sum256(data)

		// Save block first
		err = storage.SaveBlock(hash, data)
		require.NoError(t, err)

		// Test deleting existing block
		err = storage.DeleteBlock(hash)
		assert.NoError(t, err)

		// Verify file doesn't exist
		blockPath := filepath.Join(tmpDir, hex.EncodeToString(hash[:]))
		_, err = os.Stat(blockPath)
		assert.True(t, os.IsNotExist(err))

		// Test deleting non-existent block
		err = storage.DeleteBlock(hash)
		assert.NoError(t, err)
	})

	t.Run("ListBlocks", func(t *testing.T) {
		testDir := t.TempDir() // Use a fresh directory for this test
		storage, err := NewFileBlockStorage(testDir)
		require.NoError(t, err)

		// Test empty directory
		blocks, err := storage.ListBlocks()
		assert.NoError(t, err)
		assert.Empty(t, blocks)

		// Save multiple blocks
		testData := [][]byte{
			[]byte("data1"),
			[]byte("data2"),
			[]byte("data3"),
		}
		savedHashes := make([][32]byte, len(testData))
		for i, data := range testData {
			hash := sha256.Sum256(data)
			savedHashes[i] = hash
			err = storage.SaveBlock(hash, data)
			require.NoError(t, err)
		}

		// Test listing blocks
		blocks, err = storage.ListBlocks()
		assert.NoError(t, err)
		assert.Len(t, blocks, len(testData))

		// Create invalid filename
		invalidFile := filepath.Join(testDir, "invalid_hash")
		_, err = os.Create(invalidFile)
		require.NoError(t, err)

		// Test listing with invalid filename
		blocks, err = storage.ListBlocks()
		assert.NoError(t, err)
		assert.Len(t, blocks, len(testData))

		// Create directory to test directory skipping
		err = os.Mkdir(filepath.Join(testDir, "test_dir"), 0755)
		require.NoError(t, err)

		blocks, err = storage.ListBlocks()
		assert.NoError(t, err)
		assert.Len(t, blocks, len(testData))
	})

	t.Run("Concurrent operations", func(t *testing.T) {
		storage, err := NewFileBlockStorage(tmpDir)
		require.NoError(t, err)

		const goroutines = 10
		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func(id int) {
				defer wg.Done()
				data := []byte(fmt.Sprintf("concurrent data %d", id))
				hash := sha256.Sum256(data)

				err := storage.SaveBlock(hash, data)
				assert.NoError(t, err)

				loadedData, err := storage.LoadBlock(hash)
				assert.NoError(t, err)
				assert.Equal(t, data, loadedData)

				err = storage.DeleteBlock(hash)
				assert.NoError(t, err)
			}(i)
		}

		wg.Wait()
	})
}
