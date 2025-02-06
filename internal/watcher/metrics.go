package watcher

import (
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

type WatcherMetrics struct {
	eventsProcessed int64
	filesIndexed    int64
	errors          int64
	filesWatched    int64
	dirsWatched     int64
	lastEventTime   time.Time
}

func NewWatcherMetrics() *WatcherMetrics {
	return &WatcherMetrics{}
}

func (m *WatcherMetrics) RecordEvent(op fsnotify.Op) {
	atomic.AddInt64(&m.eventsProcessed, 1)
	m.lastEventTime = time.Now()
}

func (m *WatcherMetrics) RecordFileIndexed() {
	atomic.AddInt64(&m.filesIndexed, 1)
}

func (m *WatcherMetrics) RecordError() {
	atomic.AddInt64(&m.errors, 1)
}

func (m *WatcherMetrics) RecordFileAdded() {
	atomic.AddInt64(&m.filesWatched, 1)
}

func (m *WatcherMetrics) RecordDirectoryAdded() {
	atomic.AddInt64(&m.dirsWatched, 1)
}

func (m *WatcherMetrics) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"events_processed": atomic.LoadInt64(&m.eventsProcessed),
		"files_indexed":    atomic.LoadInt64(&m.filesIndexed),
		"errors":           atomic.LoadInt64(&m.errors),
		"files_watched":    atomic.LoadInt64(&m.filesWatched),
		"dirs_watched":     atomic.LoadInt64(&m.dirsWatched),
		"last_event_time":  m.lastEventTime,
	}
}
