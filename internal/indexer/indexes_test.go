package indexer

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockIndexDB struct {
	mock.Mock
}

func (m *MockIndexDB) UpdateFileIndex(fi *FileIndex) error {
	args := m.Called(fi)
	return args.Error(0)
}

func (m *MockIndexDB) GetFileIndex(path string) (*FileIndex, error) {
	args := m.Called(path)
	if fi, ok := args.Get(0).(*FileIndex); ok {
		return fi, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockIndexDB) GetAllFileIndexes() ([]*FileIndex, error) {
	args := m.Called()
	return args.Get(0).([]*FileIndex), args.Error(1)
}

func (m *MockIndexDB) Close() error {
	args := m.Called()
	return args.Error(0)
}

type MockBlockStorage struct {
	mock.Mock
}

func (m *MockBlockStorage) SaveBlock(hash [32]byte, data []byte) error {
	args := m.Called(hash, data)
	return args.Error(0)
}

func (m *MockBlockStorage) LoadBlock(hash [32]byte) ([]byte, error) {
	args := m.Called(hash)
	if b, ok := args.Get(0).([]byte); ok {
		return b, args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockBlockStorage) DeleteBlock(hash [32]byte) error {
	args := m.Called(hash)
	return args.Error(0)
}

func (m *MockBlockStorage) ListBlocks() ([][32]byte, error) {
	args := m.Called()
	return args.Get(0).([][32]byte), args.Error(1)
}

type MockFileHasher struct {
	mock.Mock
}

func (m *MockFileHasher) HashFile(path string) ([]BlockHash, [32]byte, error) {
	args := m.Called(path)
	return args.Get(0).([]BlockHash), args.Get(1).([32]byte), args.Error(2)
}

func (m *MockFileHasher) CalculateFileHash(blocks []BlockHash) ([32]byte, error) {
	args := m.Called(blocks)
	return args.Get(0).([32]byte), args.Error(1)
}

func TestRollbackToVersion(t *testing.T) {
	db := new(MockIndexDB)
	storage := new(MockBlockStorage)
	hasher := new(MockFileHasher)

	idx := NewIndexer(IndexerConfig{
		Database:     db,
		BlockStorage: storage,
		Hasher:       hasher,
	})

	tmpFile, err := os.CreateTemp("", "test_*.txt")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	blockData := []byte("test data")
	blockHash := sha256.Sum256(blockData)
	blocks := []BlockHash{{Index: 0, Hash: blockHash}}
	versionID := uuid.New().String()

	fileIndex := &FileIndex{
		Path:     tmpFile.Name(),
		Size:     int64(len(blockData)),
		ModTime:  time.Now(),
		Blocks:   blocks,
		FileHash: sha256.Sum256(blockData),
		Versions: []FileVersion{{
			VersionID: versionID,
			Blocks:    blocks,
		}},
	}

	db.On("GetFileIndex", tmpFile.Name()).Return(fileIndex, nil)
	storage.On("LoadBlock", blockHash).Return(blockData, nil)
	db.On("UpdateFileIndex", mock.MatchedBy(func(fi *FileIndex) bool {
		return fi.Path == tmpFile.Name()
	})).Return(nil)

	err = idx.RollbackToVersion(tmpFile.Name(), versionID)
	require.NoError(t, err)

	data, err := os.ReadFile(tmpFile.Name())
	require.NoError(t, err)
	assert.Equal(t, blockData, data)

	db.AssertExpectations(t)
	storage.AssertExpectations(t)
}

func TestIndexFile(t *testing.T) {
	db := new(MockIndexDB)
	storage := new(MockBlockStorage)
	hasher := new(MockFileHasher)

	idx := NewIndexer(IndexerConfig{
		Database:     db,
		BlockStorage: storage,
		Hasher:       hasher,
	})

	tmpFile, err := os.CreateTemp("", "test_*.txt")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	content := []byte("test content")
	require.NoError(t, tmpFile.Close())

	blocks := []BlockHash{{
		Index: 0,
		Hash:  sha256.Sum256(content),
		Data:  content,
	}}

	fileHash := sha256.Sum256(content)

	hasher.On("HashFile", tmpFile.Name()).Return(blocks, fileHash, nil)
	storage.On("SaveBlock", blocks[0].Hash, blocks[0].Data).Return(nil)
	db.On("GetFileIndex", tmpFile.Name()).Return(nil, ErrFileIndexNotFound)
	db.On("UpdateFileIndex", mock.AnythingOfType("*indexer.FileIndex")).Return(nil)

	err = idx.IndexFile(tmpFile.Name())
	assert.NoError(t, err)

	hasher.AssertExpectations(t)
	storage.AssertExpectations(t)
	db.AssertExpectations(t)
}

func TestIndexDirectory(t *testing.T) {
	mockDB := new(MockIndexDB)
	mockStorage := new(MockBlockStorage)
	mockHasher := new(MockFileHasher)

	indexer := NewIndexer(IndexerConfig{
		Database:     mockDB,
		BlockStorage: mockStorage,
		Hasher:       mockHasher,
	})

	// Create temporary directory with test files
	tmpDir, err := os.MkdirTemp("", "test_dir_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create test files
	files := []string{"file1.txt", "file2.txt"}
	for _, fname := range files {
		f, err := os.Create(filepath.Join(tmpDir, fname))
		require.NoError(t, err)
		_, err = f.Write([]byte("test content"))
		require.NoError(t, err)
		f.Close()
	}

	// Test data
	blocks := []BlockHash{{Index: 0, Hash: sha256.Sum256([]byte("test content"))}}
	fileHash := sha256.Sum256([]byte("test content"))

	// Setup expectations
	mockDB.On("GetAllFileIndexes").Return([]*FileIndex{}, nil)
	mockHasher.On("HashFile", mock.AnythingOfType("string")).Return(blocks, fileHash, nil)
	mockStorage.On("SaveBlock", mock.AnythingOfType("[32]uint8"), mock.AnythingOfType("[]uint8")).Return(nil)
	mockDB.On("UpdateFileIndex", mock.AnythingOfType("*indexer.FileIndex")).Return(nil)

	// Execute
	err = indexer.IndexDirectory(tmpDir)

	// Assert
	assert.NoError(t, err)
	mockDB.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
	mockHasher.AssertExpectations(t)
}

func TestUpdateFileIndexAfterBlockWrite(t *testing.T) {
	db := new(MockIndexDB)
	storage := new(MockBlockStorage)
	hasher := new(MockFileHasher)

	idx := NewIndexer(IndexerConfig{
		Database:     db,
		BlockStorage: storage,
		Hasher:       hasher,
	})

	path := "test.txt"
	blockHash := sha256.Sum256([]byte("new data"))
	fileIndex := &FileIndex{
		Path:   path,
		Blocks: []BlockHash{{Index: 0, Hash: sha256.Sum256([]byte("old data"))}},
	}

	db.On("GetFileIndex", path).Return(fileIndex, nil)
	hasher.On("CalculateFileHash", mock.AnythingOfType("[]indexer.BlockHash")).Return(sha256.Sum256([]byte("new file hash")), nil)
	db.On("UpdateFileIndex", mock.AnythingOfType("*indexer.FileIndex")).Return(nil)

	err := idx.UpdateFileIndexAfterBlockWrite(path, 0, blockHash)
	assert.NoError(t, err)

	db.AssertExpectations(t)
	hasher.AssertExpectations(t)
}
