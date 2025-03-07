package watcher

import (
	"sync"
	"time"
)

type Debouncer struct {
	duration time.Duration
	timers   map[string]*time.Timer
	mu       sync.Mutex
}

func NewDebouncer(duration time.Duration) *Debouncer {
	return &Debouncer{
		duration: duration,
		timers:   make(map[string]*time.Timer),
	}
}

func (d *Debouncer) Debounce(key string, fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if timer, exists := d.timers[key]; exists {
		timer.Stop()
	}

	d.timers[key] = time.AfterFunc(d.duration, func() {
		d.mu.Lock()
		delete(d.timers, key)
		d.mu.Unlock()
		fn()
	})
}
