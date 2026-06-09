package analyzer

import (
	"context"
	"sync"
	"time"
)

const (
	defaultN1Threshold = 5
	defaultN1Window    = time.Second
	n1CleanEvery       = time.Minute
)

// N1Detector трекает повторяющиеся запросы в рамках одного соединения
type N1Detector struct {
	mu        sync.Mutex
	threshold int
	window    time.Duration
	// connID → fingerprint → список временных меток
	records map[uint64]map[string][]time.Time
	done    chan struct{}
}

func newN1Detector(ctx context.Context, threshold int) *N1Detector {
	if threshold <= 0 {
		threshold = defaultN1Threshold
	}
	d := &N1Detector{
		threshold: threshold,
		window:    defaultN1Window,
		records:   make(map[uint64]map[string][]time.Time),
		done:      make(chan struct{}),
	}
	go d.cleanupLoop(ctx)
	return d
}

// Record фиксирует вызов запроса.
// Возвращает true ТОЛЬКО при первом превышении порога (ровно == threshold)
// чтобы не спамить алертами при каждом последующем вызове
func (d *N1Detector) Record(connID uint64, fingerprint string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-d.window)

	if d.records[connID] == nil {
		d.records[connID] = make(map[string][]time.Time)
	}

	d.records[connID][fingerprint] = append(d.records[connID][fingerprint], now)

	ts := d.records[connID][fingerprint]
	filtered := ts[:0]
	for _, t := range ts {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	d.records[connID][fingerprint] = filtered

	return len(filtered) == d.threshold
}

// ForgetConn удаляет все записи соединения при дисконнекте
func (d *N1Detector) ForgetConn(connID uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.records, connID)
}

// Stop terminates the cleanup goroutine
func (d *N1Detector) Stop() {
	select {
	case <-d.done:
	default:
		close(d.done)
	}
}

func (d *N1Detector) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(n1CleanEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.cleanup()
		case <-d.done:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (d *N1Detector) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-d.window)

	for connID, fps := range d.records {
		for fp, timestamps := range fps {
			filtered := timestamps[:0]
			for _, t := range timestamps {
				if t.After(cutoff) {
					filtered = append(filtered, t)
				}
			}
			if len(filtered) == 0 {
				delete(fps, fp)
			} else {
				fps[fp] = filtered
			}
		}
		if len(fps) == 0 {
			delete(d.records, connID)
		}
	}
}
