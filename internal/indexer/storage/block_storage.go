package storage

import (
	"encoding/hex"
	"fmt"
	"fs/internal/indexer"
	"os"
	"path/filepath"
	"sync"
)

type FileBlockStorage struct {
	baseDir string
	mu      sync.RWMutex
}

func NewFileBlockStorage(baseDir string) (*FileBlockStorage, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create blocks directory: %w", err)
	}
	return &FileBlockStorage{baseDir: baseDir}, nil
}

func (s *FileBlockStorage) SaveBlock(hash [32]byte, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	blockPath := filepath.Join(s.baseDir, hex.EncodeToString(hash[:]))
	if _, err := os.Stat(blockPath); err == nil {
		return nil // Block already exists
	}
	return os.WriteFile(blockPath, data, indexer.DefaultPermissions)
}

func (s *FileBlockStorage) LoadBlock(hash [32]byte) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	blockPath := filepath.Join(s.baseDir, hex.EncodeToString(hash[:]))
	data, err := os.ReadFile(blockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, indexer.ErrBlockNotFound
		}
		return nil, fmt.Errorf("failed to read block: %w", err)
	}
	return data, nil
}

func (s *FileBlockStorage) DeleteBlock(hash [32]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	blockPath := filepath.Join(s.baseDir, hex.EncodeToString(hash[:]))
	err := os.Remove(blockPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete block: %w", err)
	}
	return nil
}

func (s *FileBlockStorage) ListBlocks() ([][32]byte, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read blocks directory: %w", err)
	}

	blocks := make([][32]byte, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		hashStr := entry.Name()
		hashBytes, err := hex.DecodeString(hashStr)
		if err != nil {
			continue // Skip invalid filenames
		}

		var hash [32]byte
		copy(hash[:], hashBytes)
		blocks = append(blocks, hash)
	}

	return blocks, nil
}
