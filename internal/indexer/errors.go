package indexer

import "errors"

var (
	ErrFileIndexNotFound = errors.New("file index not found")
	ErrVersionNotFound   = errors.New("version not found")
	ErrInvalidBlockHash  = errors.New("invalid block hash")
	ErrBlockNotFound     = errors.New("block not found")
	ErrInvalidPath       = errors.New("invalid file path")
)
