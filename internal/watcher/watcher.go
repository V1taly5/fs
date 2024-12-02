package watcher

import (
	"fmt"
	"fs/internal/fileindex"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// type FileEvent struct {
// 	Path  string
// 	Event fsnotify.Op
// }

type FileWatcher struct {
	watcher *fsnotify.Watcher
	// events           chan FileEvent
	// errors           chan error
	indexer          *fileindex.Indexer
	debounce         map[string]*time.Timer
	debounceDuration time.Duration
	mutex            sync.Mutex
}

func NewFileWatcher(idx *fileindex.Indexer, debounceDuration time.Duration) (*FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	fw := &FileWatcher{
		watcher: watcher,
		// events:           make(chan FileEvent),
		// errors:           make(chan error),
		indexer:          idx,
		debounce:         make(map[string]*time.Timer),
		debounceDuration: debounceDuration,
	}

	// go fw.run()
	return fw, nil
}

func (fw *FileWatcher) Watch(path string) error {
	return fw.watcher.Add(path)
}

// func (fw *FileWatcher) Events() <-chan FileEvent {
// 	return fw.events
// }

// func (fw *FileWatcher) Errors() <-chan error {
// 	return fw.errors
// }

func (fw *FileWatcher) Close() error {
	return fw.watcher.Close()
}

// func (fw *FileWatcher) run() {
// 	for {
// 		select {
// 		case event, ok := <-fw.watcher.Events:
// 			if !ok {
// 				return
// 			}
// 			fw.events <- FileEvent{Path: event.Name, Event: event.Op}
// 		case err, ok := <-fw.watcher.Errors:
// 			if !ok {
// 				return
// 			}
// 			fw.errors <- err
// 		}
// 	}
// }

func (fw *FileWatcher) StartWatching(path string) error {
	err := fw.Watch(path)
	if err != nil {
		return err
	}

	go fw.handleFileEvents()
	fmt.Println("StartWatching...")
	return nil
}

func (fw *FileWatcher) handleFileEvents() {

	for {
		select {
		case event := <-fw.watcher.Events:
			path := event.Name

			// Игнорируем файлы с суффиксом ":Zone.Identifier"
			if strings.Contains(path, ":Zone.Identifier") {
				log.Printf("Ignoring file: %s", path)
				continue
			}
			if strings.Contains(path, ".swp") {
				log.Printf("Ignoring file: %s", path)
				continue
			}

			// Обрабатываем только интересующие нас события
			// if !(event.Has(fsnotify.Create) || event.Has(fsnotify.Write) || event.Has(fsnotify.Rename)) {
			// 	continue
			// }
			if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
				continue
			}
			log.Printf("File event: %s %s", event.Op, event.Name)

			fw.mutex.Lock()
			// Дебаунсинг: если таймер уже существует, сбрасываем его
			if timer, exists := fw.debounce[path]; exists {
				timer.Reset(fw.debounceDuration)
			} else {
				// Создаем новый таймер
				timer := time.AfterFunc(fw.debounceDuration, func() {
					fw.mutex.Lock()
					defer fw.mutex.Unlock()
					fw.indexer.IndexFile(path)
					delete(fw.debounce, path)
				})
				fw.debounce[path] = timer
			}

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}
