package indexer

type IndexDatabase interface {
	UpdateFileIndex(fi *FileIndex) error
	GetFileIndex(path string) (*FileIndex, error)
	GetAllFileIndexes() ([]*FileIndex, error)
	Close() error
}

type BlockStorage interface {
	SaveBlock(hash [32]byte, data []byte) error
	LoadBlock(hash [32]byte) ([]byte, error)
	DeleteBlock(hash [32]byte) error
	ListBlocks() ([][32]byte, error)
}

type FileHasher interface {
	HashFile(path string) ([]BlockHash, [32]byte, error)
	CalculateFileHash(blocks []BlockHash) ([32]byte, error)
}
