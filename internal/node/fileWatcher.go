package node

import (
	"fmt"
	"fs/internal/indexer"
	"log"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type FileEvent struct {
	Path  string
	Event fsnotify.Op
}

type FileWatcher struct {
	watcher *fsnotify.Watcher
	events  chan FileEvent
	errors  chan error
	indexer *indexer.Indexer
}

func NewFileWatcher(idx *indexer.Indexer) (*FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	fw := &FileWatcher{
		watcher: watcher,
		events:  make(chan FileEvent),
		errors:  make(chan error),
		indexer: idx,
	}

	go fw.run()
	return fw, nil
}

func (fw *FileWatcher) Watch(path string) error {
	return fw.watcher.Add(path)
}

func (fw *FileWatcher) Events() <-chan FileEvent {
	return fw.events
}

func (fw *FileWatcher) Errors() <-chan error {
	return fw.errors
}

func (fw *FileWatcher) Close() error {
	return fw.watcher.Close()
}

func (fw *FileWatcher) run() {
	for {
		select {
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			fw.events <- FileEvent{Path: event.Name, Event: event.Op}
		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			fw.errors <- err
		}
	}
}

func StartWatching(path string, db indexer.IndexDatabase) (*FileWatcher, error) {
	idx := indexer.NewIndexer(db)
	watcher, err := NewFileWatcher(idx)
	err = watcher.Watch(path)
	if err != nil {
		return nil, err
	}

	go watcher.handleFileEvents()
	fmt.Println("StartWatching...")
	return watcher, nil
}

func (fw *FileWatcher) handleFileEvents() {
	eventDebounce := make(map[string]*time.Timer)
	debounceDuration := 500 * time.Millisecond // Настраиваемая задержка для дебаунсинга

	for {
		select {
		case event := <-fw.Events():
			path := event.Path

			// Игнорируем файлы с суффиксом ":Zone.Identifier"
			if strings.Contains(path, ":Zone.Identifier") {
				log.Printf("Ignoring file: %s", path)
				continue
			}

			// Обрабатываем только интересующие нас события
			if event.Event&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
				continue
			}
			log.Printf("File event: %s %s", event.Event, event.Path)

			// Дебаунсинг: если таймер уже существует, сбрасываем его
			if timer, exists := eventDebounce[path]; exists {
				timer.Reset(debounceDuration)
			} else {
				// Создаем новый таймер
				timer := time.AfterFunc(debounceDuration, func() {
					fw.indexer.IndexFile(path)
					delete(eventDebounce, path)
				})
				eventDebounce[path] = timer
			}

		case err := <-fw.Errors():
			log.Printf("Watcher error: %v", err)
		}
	}
}
