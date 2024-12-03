package db

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"sync"

	"go.etcd.io/bbolt"
)

type IndexDB struct {
	db *bbolt.DB
	mu sync.RWMutex
}

func NewIndexDB(path string) (*IndexDB, error) {
	db, err := bbolt.Open(path, 0666, nil)
	if err != nil {
		return nil, err
	}
	return &IndexDB{db: db}, nil
}

func (idb *IndexDB) Close() error {
	return idb.db.Close()
}

// UpdateFileIndex сохраняет индекс файла в базу данных
func (idb *IndexDB) UpdateFileIndex(fi *FileIndex) error {
	idb.mu.Lock()
	defer idb.mu.Unlock()

	// Открываем транзакцию на запись
	err := idb.db.Update(func(tx *bbolt.Tx) error {
		// Получаем или создаем бакет для хранения индексов файлов
		bucket, err := tx.CreateBucketIfNotExists([]byte("file_indexes"))
		if err != nil {
			return err
		}

		// Сериализуем FileIndex с помощью gob
		var buf bytes.Buffer
		encoder := gob.NewEncoder(&buf)
		if err := encoder.Encode(fi); err != nil {
			return err
		}

		// Используем путь файла как ключ
		key := []byte(fi.Path)

		// Сохраняем сериализованные данные в бакет
		return bucket.Put(key, buf.Bytes())
	})

	return err
}

// GetFileIndex загружает индекс файла из базы данных по его пути
func (idb *IndexDB) GetFileIndex(path string) (*FileIndex, error) {
	idb.mu.RLock()
	defer idb.mu.RUnlock()

	var fi *FileIndex

	// Открываем транзакцию на чтение
	err := idb.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("file_indexes"))
		if bucket == nil {
			return fmt.Errorf("bucket not found")
		}

		key := []byte(path)
		data := bucket.Get(key)
		if data == nil {
			return fmt.Errorf("file index not found for path: %s", path)
		}

		// Десериализуем данные в FileIndex
		buf := bytes.NewReader(data)
		decoder := gob.NewDecoder(buf)
		var index FileIndex
		if err := decoder.Decode(&index); err != nil {
			return err
		}
		fi = &index
		return nil
	})

	if err != nil {
		return nil, err
	}

	return fi, nil
}

// GetAllFileIndexes возвращает все индексы файлов из базы данных
func (idb *IndexDB) GetAllFileIndexes() ([]*FileIndex, error) {
	idb.mu.RLock()
	defer idb.mu.RUnlock()

	var files []*FileIndex

	err := idb.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("file_indexes"))
		if bucket == nil {
			return nil // Нет индексов
		}

		return bucket.ForEach(func(k, v []byte) error {
			buf := bytes.NewReader(v)
			decoder := gob.NewDecoder(buf)
			var index FileIndex
			if err := decoder.Decode(&index); err != nil {
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
