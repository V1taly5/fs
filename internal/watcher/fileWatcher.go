package watcher

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type FileWatcher struct {
	watcher   *fsnotify.Watcher
	indexer   Indexer
	errors    chan error
	config    Config
	logger    *log.Logger
	debouncer *Debouncer
	// metrics   *WatcherMetrics
	stopChan chan struct{}
	wg       sync.WaitGroup
	mu       sync.RWMutex
}

func NewFileWatcher(idx Indexer, config Config) (*FileWatcher, error) {
	if config.DebounceDuration == 0 {
		config.DebounceDuration = DefaultDebounceDuration
	}
	if config.BufferSize == 0 {
		config.BufferSize = DefaultBufferSize
	}
	if config.Logger == nil {
		config.Logger = log.New(os.Stdout, "[Watcher] ", log.LstdFlags)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	fw := &FileWatcher{
		watcher: watcher,
		indexer: idx,
		// events:    make(chan FileEvent, config.BufferSize),
		errors:    make(chan error, config.BufferSize),
		config:    config,
		logger:    config.Logger,
		debouncer: NewDebouncer(config.DebounceDuration),
		// metrics:   NewWatcherMetrics(),
		stopChan: make(chan struct{}),
	}

	fw.wg.Add(1)
	go fw.run()

	return fw, nil
}

func (fw *FileWatcher) Watch(path string) error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	// Проверяем существование пути
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}

	// Если это директория, добавляем рекурсивно
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if err := fw.watcher.Add(path); err != nil {
					return fmt.Errorf("failed to watch directory %s: %w", path, err)
				}
				// fw.metrics.RecordDirectoryAdded()
			}
			return nil
		})
	}

	// Добавляем отдельный файл
	if err := fw.watcher.Add(path); err != nil {
		return fmt.Errorf("failed to watch file %s: %w", path, err)
	}

	// fw.metrics.RecordFileAdded()
	return nil
}

func (fw *FileWatcher) run() {
	defer fw.wg.Done()
	defer close(fw.errors)

	for {
		select {
		case <-fw.stopChan:
			return
		case event, ok := <-fw.watcher.Events:
			fmt.Println(event)
			if !ok {
				return
			}
			if fw.shouldProcessEvent(event) {
				fw.processEvent(event)
			}
		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			fw.handleError(err)
		}
	}
}

func (fw *FileWatcher) shouldProcessEvent(event fsnotify.Event) bool {
	// Проверяем, что это событие, которое нас интересует
	if event.Op&WatchedEvents == 0 {
		return false
	}

	// Проверяем игнорируемые паттерны
	for _, pattern := range fw.config.IgnorePatterns {
		if strings.Contains(event.Name, pattern) {
			fw.logger.Printf("Ignoring file: %s (matched pattern: %s)", event.Name, pattern)
			return false
		}
	}

	return true
}

func (fw *FileWatcher) processEvent(event fsnotify.Event) {
	// fw.metrics.RecordEvent(event.Op)

	// Используем дебаунсер для обработки события
	fw.debouncer.Debounce(event.Name, func() {
		if err := fw.indexer.IndexFile(event.Name); err != nil {
			fw.handleError(fmt.Errorf("failed to index file %s: %w", event.Name, err))
			return
		}
		// fw.metrics.RecordFileIndexed()
	})
}

func (fw *FileWatcher) handleError(err error) {
	// fw.metrics.RecordError()

	select {
	case fw.errors <- err:
	default:
		fw.logger.Printf("Error buffer full, dropping error: %v", err)
	}
}

func (fw *FileWatcher) Close() error {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	close(fw.stopChan)
	fw.wg.Wait()

	if err := fw.watcher.Close(); err != nil {
		return fmt.Errorf("failed to close watcher: %w", err)
	}

	return nil
}

func (fw *FileWatcher) Errors() <-chan error {
	return fw.errors
}

// func (fw *FileWatcher) Metrics() *WatcherMetrics {
// 	return fw.metrics
// }

// func StartWatching(path string, db indexer.IndexDatabase) (*FileWatcher, error) {
// 	idx := indexer.NewIndexer(db)
// 	watcher, err := NewFileWatcher(idx)
// 	if err != nil {
// 		return nil, err
// 	}
// 	err = watcher.Watch(path)
// 	if err != nil {
// 		return nil, err
// 	}

// 	go watcher.handleFileEvents()
// 	fmt.Println("StartWatching...")
// 	return watcher, nil
// }

// func (fw *FileWatcher) handleFileEvents() {
// 	eventDebounce := make(map[string]*time.Timer)
// 	debounceDuration := 500 * time.Millisecond // Настраиваемая задержка для дебаунсинга

// 	for {
// 		select {
// 		case event := <-fw.Events():
// 			path := event.Path

// 			// Игнорируем файлы с суффиксом ":Zone.Identifier"
// 			if strings.Contains(path, ":Zone.Identifier") {
// 				log.Printf("Ignoring file: %s", path)
// 				continue
// 			}

// 			// Обрабатываем только интересующие нас события
// 			if event.Event&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
// 				continue
// 			}
// 			log.Printf("File event: %s %s", event.Event, event.Path)

// 			// Дебаунсинг: если таймер уже существует, сбрасываем его
// 			if timer, exists := eventDebounce[path]; exists {
// 				timer.Reset(debounceDuration)
// 			} else {
// 				// Создаем новый таймер
// 				timer := time.AfterFunc(debounceDuration, func() {
// 					fw.indexer.IndexFile(path)
// 					delete(eventDebounce, path)
// 				})
// 				eventDebounce[path] = timer
// 			}

// 		case err := <-fw.Errors():
// 			log.Printf("Watcher error: %v", err)
// 		}
// 	}
// }
