package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockIndexer implements Indexer interface for testing
type MockIndexer struct {
	mock.Mock
}

func (m *MockIndexer) IndexFile(path string) error {
	args := m.Called(path)
	return args.Error(0)
}

func TestNewFileWatcher(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
	}{
		{
			name:        "Default configuration",
			config:      Config{},
			expectError: false,
		},
		{
			name: "Custom configuration",
			config: Config{
				DebounceDuration: time.Second,
				BufferSize:       200,
				IgnorePatterns:   []string{".tmp"},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockIndexer := new(MockIndexer)
			fw, err := NewFileWatcher(mockIndexer, tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, fw)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, fw)
				assert.NotNil(t, fw.watcher)
				assert.NotNil(t, fw.errors)
				assert.NotNil(t, fw.debouncer)

				// Cleanup
				err = fw.Close()
				assert.NoError(t, err)
			}
		})
	}
}

func TestFileWatcher_Watch(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "watcher_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test files
	testFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	tests := []struct {
		name        string
		path        string
		expectError bool
	}{
		{
			name:        "Watch existing directory",
			path:        tempDir,
			expectError: false,
		},
		{
			name:        "Watch existing file",
			path:        testFile,
			expectError: false,
		},
		{
			name:        "Watch non-existent path",
			path:        "/non/existent/path",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockIndexer := new(MockIndexer)
			fw, err := NewFileWatcher(mockIndexer, Config{})
			require.NoError(t, err)
			defer fw.Close()

			err = fw.Watch(tt.path)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFileWatcher_FileModifications(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "watcher_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	testFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	mockIndexer := new(MockIndexer)
	// Мок для любого пути файла
	mockIndexer.On("IndexFile", mock.AnythingOfType("string")).Return(nil)

	fw, err := NewFileWatcher(mockIndexer, Config{
		DebounceDuration: 100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer fw.Close()

	// Watch the directory
	err = fw.Watch(tempDir)
	require.NoError(t, err)

	// Отслеживаем файл
	err = fw.Watch(testFile)
	require.NoError(t, err)

	// Модифицируем существующий файл
	err = os.WriteFile(testFile, []byte("updated content"), 0644)
	require.NoError(t, err)

	// Создаем новый файл
	newFile := filepath.Join(tempDir, "new.txt")
	err = os.WriteFile(newFile, []byte("new content"), 0644)
	require.NoError(t, err)

	// Переименовываем файл
	renamedFile := filepath.Join(tempDir, "renamed.txt")
	err = os.Rename(newFile, renamedFile)
	require.NoError(t, err)

	// Ждем дебаунсер
	time.Sleep(200 * time.Millisecond)

	// Проверяем, что индексер был вызван для каждого события
	mockIndexer.AssertNumberOfCalls(t, "IndexFile", 3)
}

func TestFileWatcher_ProcessEvent(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "watcher_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	testFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	require.NoError(t, err)

	mockIndexer := new(MockIndexer)
	mockIndexer.On("IndexFile", mock.AnythingOfType("string")).Return(nil)

	fw, err := NewFileWatcher(mockIndexer, Config{
		DebounceDuration: 100 * time.Millisecond,
	})
	require.NoError(t, err)
	defer fw.Close()

	// Watch the directory
	err = fw.Watch(tempDir)
	require.NoError(t, err)

	// Create a new file to trigger events
	newFile := filepath.Join(tempDir, "new.txt")
	err = os.WriteFile(newFile, []byte("new content"), 0644)
	require.NoError(t, err)

	// Wait for debouncer
	time.Sleep(200 * time.Millisecond)

	// Verify that the indexer was called
	mockIndexer.AssertExpectations(t)
}

func TestDebouncer(t *testing.T) {
	debouncer := NewDebouncer(100 * time.Millisecond)
	counter := 0

	// Function that increments counter
	incrementCounter := func() {
		counter++
	}

	// Call debounce multiple times rapidly
	for i := 0; i < 5; i++ {
		debouncer.Debounce("test", incrementCounter)
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for debouncer timeout
	time.Sleep(200 * time.Millisecond)

	// Counter should be incremented only once
	assert.Equal(t, 1, counter)
}

func TestFileWatcher_IgnorePatterns(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "watcher_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	mockIndexer := new(MockIndexer)
	fw, err := NewFileWatcher(mockIndexer, Config{
		IgnorePatterns: []string{".tmp", "~"},
	})
	require.NoError(t, err)
	defer fw.Close()

	// Test ignored patterns
	assert.False(t, fw.shouldProcessEvent(fsnotify.Event{
		Name: "test.tmp",
		Op:   fsnotify.Create,
	}))

	// Test non-ignored patterns
	assert.True(t, fw.shouldProcessEvent(fsnotify.Event{
		Name: "test.txt",
		Op:   fsnotify.Create,
	}))
}
