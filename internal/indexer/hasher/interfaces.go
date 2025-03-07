package hasher

type BlockStorage interface {
	SaveBlock(hash [32]byte, data []byte) error
	LoadBlock(hash [32]byte) ([]byte, error)
}
