package indexer

import (
	"encoding/gob"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

func init() {
	gob.Register(FileIndex{})
	gob.Register(BlockHash{})
	gob.Register(FileVersion{})
}

type Indexer struct {
	indexDB      IndexDatabase
	blockStorage BlockStorage
	rootDir      string
	hasher       FileHasher
	logger       *log.Logger
	mu           sync.RWMutex
}

type IndexerConfig struct {
	Database     IndexDatabase
	BlockStorage BlockStorage
	RootDir      string
	Hasher       FileHasher
	Logger       *log.Logger
}

func NewIndexer(cfg IndexerConfig) *Indexer {
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stdout, "[Indexer] ", log.LstdFlags)
	}

	return &Indexer{
		indexDB:      cfg.Database,
		blockStorage: cfg.BlockStorage,
		rootDir:      cfg.RootDir,
		hasher:       cfg.Hasher,
		logger:       cfg.Logger,
	}
}

func (i *Indexer) IndexFile(path string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	fileInfo, err := os.Stat(path)
	if err != nil {
		fmt.Println("[Error]: ", err)
		return fmt.Errorf("failed to stat file: %w", err)
	}

	blocks, fileHash, err := i.hasher.HashFile(path)
	if err != nil {
		fmt.Println("[Error]: ", err)
		return fmt.Errorf("failed to hash file: %w", err)
	}

	// Создаем новую версию
	version := i.createNewVersion(fileHash, blocks)

	// Получаем или создаем индекс файла
	fi, err := i.getOrCreateFileIndex(path, fileInfo, blocks, fileHash)
	if err != nil {
		fmt.Println("[Error]: ", err)
		return err
	}
	fmt.Println("Сохраненные блоки в бд: ", fi)

	// Добавляем новую версию
	fi = i.addNewVersion(fi, version)

	// Сохраняем блоки
	if err := i.saveFileBlocks(blocks); err != nil {
		return err
	}

	// Обновляем индекс
	if err := i.indexDB.UpdateFileIndex(fi); err != nil {
		return fmt.Errorf("failed to update file index: %w", err)
	}

	i.logger.Printf("Successfully indexed file: %s", path)
	return nil
}

// TODO: implement gradual deletion of deleted data blocks
func (i *Indexer) RemoveFileIndex(path string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	_, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	fi, err := i.indexDB.GetFileIndex(path)
	if err != nil {
		if errors.Is(err, ErrFileIndexNotFound) {
			return nil // file already not indexed
		}
		return fmt.Errorf("failed to get file index: %w", err)
	}

	fi.Deleted = true
	fi.ModTime = time.Now()

	if err := i.indexDB.UpdateFileIndex(fi); err != nil {
		return fmt.Errorf("failed to update file index: %w", err)
	}
	i.logger.Printf("Marked file as deleted: %s", path)
	return nil
}

func (i *Indexer) createNewVersion(fileHash [32]byte, blocks []BlockHash) FileVersion {
	return FileVersion{
		VersionID: uuid.New().String(),
		Timestamp: time.Now(),
		FileHash:  fileHash,
		Blocks:    blocks,
	}
}

func (i *Indexer) getOrCreateFileIndex(path string, fileInfo os.FileInfo, blocks []BlockHash, fileHash [32]byte) (*FileIndex, error) {
	existingIndex, err := i.indexDB.GetFileIndex(path)
	fmt.Println(err)
	if err != nil && !errors.Is(err, ErrFileIndexNotFound) {
		return nil, fmt.Errorf("failed to get file index: %w", err)
	}

	if err == ErrFileIndexNotFound {
		return &FileIndex{
			Path:     path,
			Size:     fileInfo.Size(),
			ModTime:  fileInfo.ModTime(),
			Blocks:   blocks,
			FileHash: fileHash,
		}, nil
	}

	existingIndex.Size = fileInfo.Size()
	existingIndex.ModTime = fileInfo.ModTime()
	existingIndex.Blocks = blocks
	existingIndex.FileHash = fileHash
	return existingIndex, nil
}

func (i *Indexer) addNewVersion(fi *FileIndex, version FileVersion) *FileIndex {
	fi.Versions = append(fi.Versions, version)
	fi.LastSyncedVersionID = version.VersionID

	// Ограничиваем количество версий
	if len(fi.Versions) > MaxVersionsPerFile {
		fi.Versions = fi.Versions[len(fi.Versions)-MaxVersionsPerFile:]
	}

	return fi
}

func (i *Indexer) saveFileBlocks(blocks []BlockHash) error {
	for idx := range blocks {
		if err := i.blockStorage.SaveBlock(blocks[idx].Hash, blocks[idx].Data); err != nil {
			return fmt.Errorf("failed to save block %x: %w", blocks[idx].Hash, err)
		}
		blocks[idx].Data = nil // Clear data after saving
	}
	return nil
}

func (i *Indexer) GetFileIndex(path string) (*FileIndex, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.indexDB.GetFileIndex(path)
}

func (i *Indexer) RollbackToVersion(path, versionID string) error {
	// First get the file index with a read lock
	i.mu.RLock()
	fi, err := i.indexDB.GetFileIndex(path)
	i.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("failed to get file index: %w", err)
	}

	// Find the version
	var targetVersion *FileVersion
	for idx := range fi.Versions {
		if fi.Versions[idx].VersionID == versionID {
			targetVersion = &fi.Versions[idx]
			break
		}
	}
	if targetVersion == nil {
		return ErrVersionNotFound
	}

	// Restore file content
	if err := i.RestoreFileFromBlocks(path, targetVersion.Blocks); err != nil {
		return err
	}

	// Update index with write lock
	i.mu.Lock()
	defer i.mu.Unlock()

	// Get fresh copy of index
	fi, err = i.indexDB.GetFileIndex(path)
	if err != nil {
		return fmt.Errorf("failed to get file index: %w", err)
	}

	fi.Blocks = targetVersion.Blocks
	fi.FileHash = targetVersion.FileHash
	fi.LastSyncedVersionID = targetVersion.VersionID
	fi.ModTime = time.Now()

	return i.indexDB.UpdateFileIndex(fi)
}

func (i *Indexer) findVersion(fi *FileIndex, versionID string) (*FileVersion, error) {
	for idx := range fi.Versions {
		if fi.Versions[idx].VersionID == versionID {
			return &fi.Versions[idx], nil
		}
	}
	return nil, ErrVersionNotFound
}

// path this is the path where the file is saved on another node (absolute path)
// based on it and the root directory, we determine where to save the file
func (i *Indexer) RestoreFileFromBlocks(path string, blocks []BlockHash) error {
	fullPath, err := i.mapToLocalPath(path)
	if err != nil {
		return fmt.Errorf("failed to map ex path to local path: %w", err)
	}

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	file, err := os.OpenFile(fullPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	for _, block := range blocks {
		data, err := i.blockStorage.LoadBlock(block.Hash)
		if err != nil {
			return fmt.Errorf("failed to load block %x: %w", block.Hash, err)
		}

		if _, err := file.Write(data); err != nil {
			return fmt.Errorf("failed to write block to file: %w", err)
		}
	}

	return nil
}
func (i *Indexer) mapToLocalPath(path string) (string, error) {
	// приводим путь к абсолютному виду
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("не удалось получить абсолютный путь: %w", err)
	}

	// разделяем путь на компоненты
	parts := strings.Split(filepath.ToSlash(absPath), "/")

	// извлекаем имя общей корневой директории из RootDir
	commonRoot := filepath.Base(filepath.ToSlash(i.rootDir))

	// ищем индекс общей корневой директории
	var idx int = -1
	for j, part := range parts {
		if part == commonRoot {
			idx = j
			break
		}
	}
	if idx == -1 {
		return "", fmt.Errorf("общая корневая директория %q не найдена в пути %q", commonRoot, absPath)
	}

	// строим относительный путь от общей корневой директории до целевого файла
	relParts := parts[idx+1:]
	relPath := filepath.Join(relParts...)

	fullPath := filepath.Join(i.rootDir, relPath)

	return fullPath, nil
}

func (i *Indexer) UpdateFileIndexAfterBlockWrite(filePath string, blockIndex int, blockHash [32]byte) error {
	// get initial index with read lock
	i.mu.RLock()
	fi, err := i.indexDB.GetFileIndex(filePath)
	i.mu.RUnlock()

	var blocks []BlockHash
	if err != nil {
		if errors.Is(err, ErrFileIndexNotFound) {
			blocks = make([]BlockHash, 0)
		} else {
			return fmt.Errorf("failed to get file index: %w", err)
		}
	} else {
		blocks := make([]BlockHash, len(fi.Blocks))
		copy(blocks, fi.Blocks)
	}

	// update block in memory
	updated := false
	for idx := range blocks {
		if blocks[idx].Index == blockIndex {
			blocks[idx].Hash = blockHash
			updated = true
			break
		}
	}

	if !updated {
		blocks = append(blocks, BlockHash{
			Index: blockIndex,
			Hash:  blockHash,
		})
	}

	// Calculate new file hash
	fileHash, err := i.hasher.CalculateFileHash(blocks)
	if err != nil {
		return fmt.Errorf("failed to calculate file hash: %w", err)
	}

	// Update with write lock
	i.mu.Lock()
	defer i.mu.Unlock()

	// Get fresh index
	// fi, err = i.indexDB.GetFileIndex(filePath)
	// if err != nil {
	// 	return fmt.Errorf("failed to get file index: %w", err)
	// }
	if fi == nil {
		fi = &FileIndex{
			Path:     filePath,
			ModTime:  time.Now(),
			Blocks:   blocks,
			FileHash: fileHash,
			Versions: make([]FileVersion, 0),
		}
	} else {
		// Получаем свежий индекс (мог измениться после RUnlock)
		freshFi, err := i.indexDB.GetFileIndex(filePath)
		if err != nil {
			if err != ErrFileIndexNotFound {
				return fmt.Errorf("failed to get fresh file index: %w", err)
			}
		} else {
			fi = freshFi
		}
		fi.Blocks = blocks
		fi.FileHash = fileHash
		fi.ModTime = time.Now()
	}
	return i.indexDB.UpdateFileIndex(fi)
}

// func (i *Indexer) UpdateFileIndexAfterBlockWrite(filePath string, blockIndex int, blockHash [32]byte) error {
// 	i.mu.Lock()
// 	defer i.mu.Unlock()

// 	fi, err := i.GetFileIndex(filePath)
// 	if err != nil {
// 		return fmt.Errorf("failed to get file index: %w", err)
// 	}

// 	// Update block in the index
// 	updated := false
// 	for idx := range fi.Blocks {
// 		if fi.Blocks[idx].Index == blockIndex {
// 			fi.Blocks[idx].Hash = blockHash
// 			updated = true
// 			break
// 		}
// 	}

// 	if !updated {
// 		fi.Blocks = append(fi.Blocks, BlockHash{
// 			Index: blockIndex,
// 			Hash:  blockHash,
// 		})
// 	}

// 	// Recalculate file hash
// 	fileHash, err := i.hasher.CalculateFileHash(fi.Blocks)
// 	if err != nil {
// 		return fmt.Errorf("failed to calculate file hash: %w", err)
// 	}
// 	fi.FileHash = fileHash
// 	fi.ModTime = time.Now()

// 	// Update index in database
// 	if err := i.indexDB.UpdateFileIndex(fi); err != nil {
// 		return fmt.Errorf("failed to update file index: %w", err)
// 	}

// 	return nil
// }

func (i *Indexer) Close() error {
	return i.indexDB.Close()
}

// IndexDirectory выполняет одноразовую индексацию всех файлов в указанной директории
func (i *Indexer) IndexDirectory(path string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Проверяем существование директории
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}

	// Получаем все существующие индексы
	existingIndexes, err := i.indexDB.GetAllFileIndexes()
	if err != nil {
		return fmt.Errorf("failed to get existing indexes: %w", err)
	}

	// Создаем мапу существующих индексов для быстрого поиска
	indexMap := make(map[string]*FileIndex)
	for _, idx := range existingIndexes {
		indexMap[idx.Path] = idx
	}

	// Рекурсивно обходим директорию
	return filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Пропускаем директории и специальные файлы
		if info.IsDir() || strings.Contains(filePath, ":Zone.Identifier") {
			return nil
		}

		// Получаем существующий индекс, если есть
		existingIndex, exists := indexMap[filePath]

		// Если файл существует и не изменился, пропускаем его
		if exists && existingIndex.ModTime.Equal(info.ModTime()) {
			return nil
		}

		// Индексируем файл
		blocks, fileHash, err := i.hasher.HashFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to hash file %s: %w", filePath, err)
		}

		// Создаем новую версию
		version := FileVersion{
			VersionID: uuid.New().String(),
			Timestamp: time.Now(),
			FileHash:  fileHash,
			Blocks:    blocks,
		}

		// Обновляем или создаем индекс
		var fi *FileIndex
		if exists {
			fi = existingIndex
			fi.Size = info.Size()
			fi.ModTime = info.ModTime()
			fi.Blocks = blocks
			fi.FileHash = fileHash
			fi.Versions = append(fi.Versions, version)

			// Ограничиваем количество версий
			if len(fi.Versions) > MaxVersionsPerFile {
				fi.Versions = fi.Versions[len(fi.Versions)-MaxVersionsPerFile:]
			}
		} else {
			fi = &FileIndex{
				Path:     filePath,
				Size:     info.Size(),
				ModTime:  info.ModTime(),
				Blocks:   blocks,
				FileHash: fileHash,
				Versions: []FileVersion{version},
			}
		}

		// Сохраняем блоки
		for idx, block := range blocks {
			if err := i.blockStorage.SaveBlock(block.Hash, block.Data); err != nil {
				return fmt.Errorf("failed to save block for file %s: %w", filePath, err)
			}
			blocks[idx].Data = nil // Очищаем данные после сохранения
		}

		fi.LastSyncedVersionID = version.VersionID

		// Обновляем индекс в базе данных
		if err := i.indexDB.UpdateFileIndex(fi); err != nil {
			return fmt.Errorf("failed to update index for file %s: %w", filePath, err)
		}

		return nil
	})
}

// TODO: normalize
func (i *Indexer) GetAllFileIndexes() ([]*FileIndex, error) {
	fileIndexes, err := i.indexDB.GetAllFileIndexes()
	if err != nil {
		return nil, err
	}

	return fileIndexes, nil
}
