package watcher

import (
	"log"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileEvent представляет событие файловой системы
type FileEvent struct {
	Path      string
	Event     fsnotify.Op
	Timestamp time.Time
}

// Config содержит настройки для FileWatcher
type Config struct {
	DebounceDuration time.Duration
	BufferSize       int
	IgnorePatterns   []string
	Logger           *log.Logger
}
