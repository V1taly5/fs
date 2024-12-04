package scaner

import (
	"fs/internal/db"
	"fs/internal/indexer"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type FileScaner struct {
	indexer *indexer.Indexer
}

// ScanAndIndexFiles рекурсивно сканирует указанную директорию и индексирует новые или измененные файлы.
func (fs *FileScaner) ScanAndIndexFiles(rootPath string) error {
	return filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Failed to access path %s: %v", path, err)
			return err
		}

		// Пропускаем директории
		if info.IsDir() {
			return nil
		}

		// Пропускаем скрытые файлы или файлы, которые должны быть проигнорированы
		if shouldIgnoreFile(path, info) {
			return nil
		}

		// Получаем существующий индекс файла из базы данных
		existingIndex, err := fs.indexer.GetFileIndex(path)
		if err != nil && err != db.ErrFileIndexNotFound {
			log.Printf("Failed to get file index for %s: %v", path, err)
			return err
		}

		// Если файл не индексирован или был изменен, индексируем его
		if existingIndex == nil || existingIndex.ModTime != info.ModTime() || existingIndex.Size != info.Size() {
			log.Printf("Indexing file: %s", path)
			fs.indexer.IndexFile(path)
		}

		return nil
	})
}

// Вспомогательный метод для определения, нужно ли игнорировать файл
func shouldIgnoreFile(path string, info os.FileInfo) bool {
	// Игнорируем файлы с определенными суффиксами или шаблонами
	// Например, можно игнорировать временные или скрытые файлы
	filename := info.Name()
	if strings.HasPrefix(filename, ".") {
		return true // Игнорируем скрытые файлы
	}

	// Добавьте дополнительные условия при необходимости
	return false
}
