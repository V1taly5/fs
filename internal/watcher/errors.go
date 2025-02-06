package watcher

import "errors"

var (
	ErrWatcherClosed      = errors.New("watcher is closed")
	ErrInvalidPath        = errors.New("invalid path")
	ErrWatcherNotFound    = errors.New("watcher not found")
	ErrPathAlreadyWatched = errors.New("path is already being watched")
)
