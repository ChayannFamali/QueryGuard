package analyzer

import (
	"sync"
	"time"
)

const (
	n1Threshold  = 5           // сколько раз в окне → N+1
	n1Window     = time.Second // временное окно наблюдения
	n1CleanEvery = time.Minute // как часто чистим старые записи
)

// N1Detector отслеживает повторяющиеся запросы в рамках одного соединения
type N1Detector struct {
	mu sync.Mutex
	// connID → fingerprint → список временных меток
	records map[uint64]map[string][]time.Time
}

func newN1Detector() *N1Detector {
	d := &N1Detector{
		records: make(map[uint64]map[string][]time.Time),
	}
	go d.cleanupLoop()
	return d
}

// Record фиксирует вызов запроса.
// Возвращает true ТОЛЬКО при первом превышении порога (ровно == threshold)
// чтобы не спамить алертами при каждом последующем вызове
func (d *N1Detector) Record(connID uint64, fingerprint string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-n1Window)

	if d.records[connID] == nil {
		d.records[connID] = make(map[string][]time.Time)
	}

	// Добавляем текущий timestamp
	d.records[connID][fingerprint] = append(d.records[connID][fingerprint], now)

	// Фильтруем timestamps вне окна
	ts := d.records[connID][fingerprint]
	filtered := ts[:0]
	for _, t := range ts {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	d.records[connID][fingerprint] = filtered

	// Алерт только при первом пересечении порога
	return len(filtered) == n1Threshold
}

// ForgetConn удаляет все записи соединения при дисконнекте
func (d *N1Detector) ForgetConn(connID uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.records, connID)
}

func (d *N1Detector) cleanupLoop() {
	ticker := time.NewTicker(n1CleanEvery)
	defer ticker.Stop()
	for range ticker.C {
		d.cleanup()
	}
}

func (d *N1Detector) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-n1Window)

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
