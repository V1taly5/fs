package cleanup

import (
	"fmt"
	"fs/internal/indexer"
)

type BlockCleaner struct {
	db           indexer.IndexDatabase
	blockStorage indexer.BlockStorage
}

func NewBlockCleaner(db indexer.IndexDatabase, storage indexer.BlockStorage) *BlockCleaner {
	return &BlockCleaner{
		db:           db,
		blockStorage: storage,
	}
}

func (c *BlockCleaner) CleanupUnusedBlocks() error {
	// Получаем все блоки из хранилища
	existingBlocks, err := c.blockStorage.ListBlocks()
	if err != nil {
		return fmt.Errorf("failed to list blocks: %w", err)
	}

	// Создаем мапу для отслеживания используемых блоков
	usedBlocks := make(map[[32]byte]bool)

	// Получаем все индексы файлов
	indexes, err := c.db.GetAllFileIndexes()
	if err != nil {
		return fmt.Errorf("failed to get file indexes: %w", err)
	}

	// Отмечаем все используемые блоки
	for _, index := range indexes {
		// Проверяем текущие блоки файла
		for _, block := range index.Blocks {
			usedBlocks[block.Hash] = true
		}

		// Проверяем блоки во всех версиях
		for _, version := range index.Versions {
			for _, block := range version.Blocks {
				usedBlocks[block.Hash] = true
			}
		}
	}

	// Удаляем неиспользуемые блоки
	for _, hash := range existingBlocks {
		if !usedBlocks[hash] {
			if err := c.blockStorage.DeleteBlock(hash); err != nil {
				return fmt.Errorf("failed to delete unused block %x: %w", hash, err)
			}
		}
	}

	return nil
}
