package db

import "fs/internal/indexer"

// FileIndexStorage определяет интерфейс для хранения индексов файлов
type FileIndexStorage interface {
	UpdateFileIndex(fi *indexer.FileIndex) error
	GetFileIndex(path string) (*indexer.FileIndex, error)
	GetAllFileIndexes() ([]*indexer.FileIndex, error)
	DeleteFileIndex(path string) error
	Close() error
}
