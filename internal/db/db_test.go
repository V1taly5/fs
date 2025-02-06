package db

import (
	"crypto/sha256"
	"fmt"
	"fs/internal/indexer"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testHelper struct {
	db  *IndexDB
	dir string
}

func setupTest(t *testing.T) *testHelper {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	cfg := Config{
		Path:       dbPath,
		FileMode:   0666,
		Serializer: &GobSerializer{},
	}

	db, err := NewIndexDB(cfg)
	require.NoError(t, err)
	require.NotNil(t, db)

	t.Cleanup(func() {
		db.Close()
	})

	return &testHelper{
		db:  db,
		dir: dir,
	}
}

func createTestBlockHash(index int, data []byte) indexer.BlockHash {
	hash := sha256.Sum256(data)
	return indexer.BlockHash{
		Index: index,
		Hash:  hash,
		Data:  data,
	}
}

func createTestFileVersion(data []byte) indexer.FileVersion {
	fileHash := sha256.Sum256(data)
	blocks := []indexer.BlockHash{
		createTestBlockHash(0, data[:len(data)/2]),
		createTestBlockHash(1, data[len(data)/2:]),
	}

	return indexer.FileVersion{
		VersionID: uuid.New().String(),
		Timestamp: time.Now(),
		FileHash:  fileHash,
		Blocks:    blocks,
	}
}

func createTestFileIndex(path string) *indexer.FileIndex {
	// Создаем тестовые данные
	testData := []byte("test file content")
	fileHash := sha256.Sum256(testData)

	// Создаем блоки
	blocks := []indexer.BlockHash{
		createTestBlockHash(0, testData[:len(testData)/2]),
		createTestBlockHash(1, testData[len(testData)/2:]),
	}

	// Создаем версии
	versions := []indexer.FileVersion{
		createTestFileVersion(testData),
		createTestFileVersion(append(testData, []byte(" updated")...)),
	}

	return &indexer.FileIndex{
		Path:                path,
		Size:                int64(len(testData)),
		ModTime:             time.Now(),
		Blocks:              blocks,
		FileHash:            fileHash,
		Versions:            versions,
		LastSyncedVersionID: versions[len(versions)-1].VersionID,
	}
}

func TestIndexDB_UpdateFileIndex(t *testing.T) {
	h := setupTest(t)

	tests := []struct {
		name        string
		input       *indexer.FileIndex
		shouldError bool
	}{
		{
			name:        "Valid file index",
			input:       createTestFileIndex("/test/file1.txt"),
			shouldError: false,
		},
		{
			name:        "Nil file index",
			input:       nil,
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := h.db.UpdateFileIndex(tt.input)
			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIndexDB_GetFileIndex(t *testing.T) {
	h := setupTest(t)

	// Prepare test data
	testIndex := createTestFileIndex("/test/file1.txt")
	err := h.db.UpdateFileIndex(testIndex)
	require.NoError(t, err)

	tests := []struct {
		name        string
		path        string
		shouldError bool
		errorType   error
		validate    func(*testing.T, *indexer.FileIndex)
	}{
		{
			name:        "Existing file",
			path:        "/test/file1.txt",
			shouldError: false,
			validate: func(t *testing.T, result *indexer.FileIndex) {
				assert.Equal(t, testIndex.Path, result.Path)
				assert.Equal(t, testIndex.Size, result.Size)
				assert.Equal(t, testIndex.FileHash, result.FileHash)
				assert.Equal(t, len(testIndex.Versions), len(result.Versions))
				assert.Equal(t, testIndex.LastSyncedVersionID, result.LastSyncedVersionID)

				// Проверяем блоки
				assert.Equal(t, len(testIndex.Blocks), len(result.Blocks))
				for i, block := range result.Blocks {
					assert.Equal(t, testIndex.Blocks[i].Index, block.Index)
					assert.Equal(t, testIndex.Blocks[i].Hash, block.Hash)
					// Data должен быть nil после десериализации
					// assert.Nil(t, block.Data)
				}

				// Проверяем версии
				for i, version := range result.Versions {
					assert.Equal(t, testIndex.Versions[i].VersionID, version.VersionID)
					assert.Equal(t, testIndex.Versions[i].FileHash, version.FileHash)
					assert.Equal(t, len(testIndex.Versions[i].Blocks), len(version.Blocks))
				}
			},
		},
		{
			name:        "Non-existing file",
			path:        "/test/nonexistent.txt",
			shouldError: true,
			errorType:   ErrFileIndexNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := h.db.GetFileIndex(tt.path)
			if tt.shouldError {
				assert.Error(t, err)
				if tt.errorType != nil {
					assert.ErrorIs(t, err, tt.errorType)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.validate != nil {
					tt.validate(t, result)
				}
			}
		})
	}
}

func TestIndexDB_GetAllFileIndexes(t *testing.T) {
	h := setupTest(t)

	// Prepare test data
	testIndexes := []*indexer.FileIndex{
		createTestFileIndex("/test/file1.txt"),
		createTestFileIndex("/test/file2.txt"),
		createTestFileIndex("/test/file3.txt"),
	}

	for _, idx := range testIndexes {
		err := h.db.UpdateFileIndex(idx)
		require.NoError(t, err)
	}

	// Test retrieval
	result, err := h.db.GetAllFileIndexes()
	assert.NoError(t, err)
	assert.Len(t, result, len(testIndexes))

	// Проверяем каждый файл
	for _, expected := range testIndexes {
		found := false
		for _, actual := range result {
			if actual.Path == expected.Path {
				found = true
				// Проверяем основные поля
				assert.Equal(t, expected.Size, actual.Size)
				assert.Equal(t, expected.FileHash, actual.FileHash)
				assert.Equal(t, expected.LastSyncedVersionID, actual.LastSyncedVersionID)

				// Проверяем версии
				assert.Len(t, actual.Versions, len(expected.Versions))
				for i, version := range actual.Versions {
					assert.Equal(t, expected.Versions[i].VersionID, version.VersionID)
					assert.Equal(t, expected.Versions[i].FileHash, version.FileHash)
				}

				// Проверяем блоки
				assert.Len(t, actual.Blocks, len(expected.Blocks))
				for i, block := range actual.Blocks {
					assert.Equal(t, expected.Blocks[i].Index, block.Index)
					assert.Equal(t, expected.Blocks[i].Hash, block.Hash)
					// assert.Nil(t, block.Data) // Data должен быть nil после десериализации
				}
				break
			}
		}
		assert.True(t, found, "File index not found: %s", expected.Path)
	}
}

func TestGobSerializer(t *testing.T) {
	serializer := &GobSerializer{}

	testIndex := createTestFileIndex("/test/file1.txt")

	// Test serialization
	data, err := serializer.Serialize(testIndex)
	assert.NoError(t, err)
	assert.NotNil(t, data)

	// Test deserialization
	var result indexer.FileIndex
	err = serializer.Deserialize(data, &result)
	assert.NoError(t, err)

	// Проверяем корректность десериализации
	assert.Equal(t, testIndex.Path, result.Path)
	assert.Equal(t, testIndex.Size, result.Size)
	assert.Equal(t, testIndex.FileHash, result.FileHash)
	assert.Equal(t, testIndex.LastSyncedVersionID, result.LastSyncedVersionID)

	// Проверяем блоки
	assert.Len(t, result.Blocks, len(testIndex.Blocks))
	for i, block := range result.Blocks {
		assert.Equal(t, testIndex.Blocks[i].Index, block.Index)
		assert.Equal(t, testIndex.Blocks[i].Hash, block.Hash)
		// assert.Nil(t, block.Data) // Проверяем, что Data не сериализуется
	}

	// Проверяем версии
	assert.Len(t, result.Versions, len(testIndex.Versions))
	for i, version := range result.Versions {
		assert.Equal(t, testIndex.Versions[i].VersionID, version.VersionID)
		assert.Equal(t, testIndex.Versions[i].FileHash, version.FileHash)
		assert.Len(t, version.Blocks, len(testIndex.Versions[i].Blocks))
	}
}

func TestIndexDB_Concurrency(t *testing.T) {
	h := setupTest(t)

	const numGoroutines = 10
	done := make(chan bool)

	// Конкурентная запись
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			index := createTestFileIndex(fmt.Sprintf("/test/file%d.txt", id))
			err := h.db.UpdateFileIndex(index)
			assert.NoError(t, err)
			done <- true
		}(i)
	}

	// Ожидаем завершения всех горутин
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Проверяем, что все файлы были сохранены
	indexes, err := h.db.GetAllFileIndexes()
	assert.NoError(t, err)
	assert.Len(t, indexes, numGoroutines)
}

func TestIndexDB_DeleteFileIndex(t *testing.T) {
	h := setupTest(t)

	// Prepare test data
	testIndex := createTestFileIndex("/test/file1.txt")
	err := h.db.UpdateFileIndex(testIndex)
	require.NoError(t, err)

	tests := []struct {
		name        string
		path        string
		shouldError bool
	}{
		{
			name:        "Delete existing file",
			path:        "/test/file1.txt",
			shouldError: false,
		},
		{
			name:        "Delete non-existing file",
			path:        "/test/nonexistent.txt",
			shouldError: false, // Удаление несуществующего файла не должно возвращать ошибку
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := h.db.DeleteFileIndex(tt.path)
			if tt.shouldError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Проверяем, что файл действительно удален
			_, err = h.db.GetFileIndex(tt.path)
			assert.ErrorIs(t, err, ErrFileIndexNotFound)
		})
	}
}
