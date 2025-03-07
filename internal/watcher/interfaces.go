package watcher

type Indexer interface {
	IndexFile(path string) error
}
