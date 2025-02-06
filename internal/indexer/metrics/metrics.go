package metrics

import (
	"sync/atomic"
	"time"
)

type IndexerMetrics struct {
	filesIndexed    int64
	blocksProcessed int64
	errors          int64
	processedBytes  int64
	lastProcessed   time.Time
}

func NewIndexerMetrics() *IndexerMetrics {
	return &IndexerMetrics{}
}

func (m *IndexerMetrics) RecordFileIndexed(path string, duration time.Duration) {
	atomic.AddInt64(&m.filesIndexed, 1)
	m.lastProcessed = time.Now()
}

func (m *IndexerMetrics) RecordBlockProcessed(size int64) {
	atomic.AddInt64(&m.blocksProcessed, 1)
	atomic.AddInt64(&m.processedBytes, size)
}

func (m *IndexerMetrics) RecordError() {
	atomic.AddInt64(&m.errors, 1)
}

func (m *IndexerMetrics) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"files_indexed":    atomic.LoadInt64(&m.filesIndexed),
		"blocks_processed": atomic.LoadInt64(&m.blocksProcessed),
		"errors":           atomic.LoadInt64(&m.errors),
		"processed_bytes":  atomic.LoadInt64(&m.processedBytes),
		"last_processed":   m.lastProcessed,
	}
}
