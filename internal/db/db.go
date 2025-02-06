package db

import (
	"fmt"
	"fs/internal/indexer"
	"os"
	"sync"

	"go.etcd.io/bbolt"
)

const (
	FileIndexesBucket = "file_indexes"
)

// IndexDB представляет базу данных индексов файлов
type IndexDB struct {
	db         *bbolt.DB
	mu         sync.RWMutex
	serializer Serializer
}

// Config содержит конфигурацию для IndexDB
type Config struct {
	Path       string
	FileMode   os.FileMode
	Options    *bbolt.Options
	Serializer Serializer
}

// NewIndexDB создает новый экземпляр IndexDB
func NewIndexDB(cfg Config) (*IndexDB, error) {
	if cfg.Serializer == nil {
		cfg.Serializer = &GobSerializer{}
	}

	if cfg.FileMode == 0 {
		cfg.FileMode = 0666
	}

	db, err := bbolt.Open(cfg.Path, cfg.FileMode, cfg.Options)
	if err != nil {
		return nil, err
	}

	// Создаем bucket при инициализации
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(FileIndexesBucket))
		if err != nil {
			return fmt.Errorf("failed to create bucket: %w", err)
		}
		return nil
	})
	if err != nil {
		db.Close() // Закрываем БД в случае ошибки
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	return &IndexDB{
		db:         db,
		serializer: cfg.Serializer,
	}, nil
}

func (idb *IndexDB) Close() error {
	if idb.db == nil {
		return ErrNilDB
	}
	return idb.db.Close()
}

// UpdateFileIndex сохраняет индекс файла в базу данных
func (idb *IndexDB) UpdateFileIndex(fi *indexer.FileIndex) error {
	if fi == nil {
		return ErrNilFileIndex
	}

	data, err := idb.serializer.Serialize(fi)
	if err != nil {
		return err
	}

	idb.mu.Lock()
	defer idb.mu.Unlock()

	return idb.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists([]byte(FileIndexesBucket))
		if err != nil {
			return err
		}
		return bucket.Put([]byte(fi.Path), data)
	})
}

// GetFileIndex загружает индекс файла из базы данных по его пути
func (idb *IndexDB) GetFileIndex(path string) (*indexer.FileIndex, error) {
	var fi indexer.FileIndex

	idb.mu.RLock()
	defer idb.mu.RUnlock()

	err := idb.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(FileIndexesBucket))
		if bucket == nil {
			return ErrBucketNotFound
		}

		data := bucket.Get([]byte(path))
		if data == nil {
			return indexer.ErrFileIndexNotFound
		}

		return idb.serializer.Deserialize(data, &fi)
	})

	if err != nil {
		return nil, err
	}
	return &fi, nil
}

// GetAllFileIndexes возвращает все индексы файлов из базы данных
func (idb *IndexDB) GetAllFileIndexes() ([]*indexer.FileIndex, error) {
	var files []*indexer.FileIndex

	idb.mu.RLock()
	defer idb.mu.RUnlock()

	err := idb.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(FileIndexesBucket))
		if bucket == nil {
			return nil
		}

		return bucket.ForEach(func(k, v []byte) error {
			var index indexer.FileIndex
			if err := idb.serializer.Deserialize(v, &index); err != nil {
				return err
			}
			files = append(files, &index)
			return nil
		})
	})

	if err != nil {
		return nil, err
	}
	return files, nil
}

// DeleteFileIndex удаляет индекс файла из базы данных
func (idb *IndexDB) DeleteFileIndex(path string) error {
	idb.mu.Lock()
	defer idb.mu.Unlock()

	return idb.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(FileIndexesBucket))
		if bucket == nil {
			return nil
		}
		return bucket.Delete([]byte(path))
	})
}
