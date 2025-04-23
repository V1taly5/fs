package indexer

import "time"

type FileIndex struct {
	Path                string
	Size                int64
	ModTime             time.Time
	Blocks              []BlockHash
	FileHash            [32]byte
	Versions            []FileVersion
	LastSyncedVersionID string
	Deleted             bool
}

type FileVersion struct {
	VersionID string
	Timestamp time.Time
	FileHash  [32]byte
	Blocks    []BlockHash
}

type BlockHash struct {
	Index int
	Hash  [32]byte
	Data  []byte `json:"-" gob:"-"`
}

func (fv *FileVersion) GetBlocks() []BlockHash {
	return fv.Blocks
}

func (fv *FileVersion) GetVersion() string {
	return fv.VersionID
}
