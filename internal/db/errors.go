package db

import "errors"

var (
	ErrFileIndexNotFound = errors.New("file index not found")
	ErrBucketNotFound    = errors.New("bucket not found")
	ErrNilDB             = errors.New("database connection is nil")
	ErrNilFileIndex      = errors.New("file index is nil")
)
