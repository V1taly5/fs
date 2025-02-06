package watcher

import (
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	DefaultDebounceDuration = 500 * time.Millisecond
	DefaultBufferSize       = 100
)

var (
	// События, за которыми мы следим
	WatchedEvents = fsnotify.Create | fsnotify.Write | fsnotify.Rename

	// Файловые паттерны, которые нужно игнорировать
	IgnoredPatterns = []string{
		":Zone.Identifier",
		".tmp",
		"~",
	}
)
