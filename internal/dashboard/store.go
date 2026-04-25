package dashboard

import (
	"sync"
	"sync/atomic"
	"time"
)

const MaxEntries = 500

type QueryEntry struct {
	ID          uint64
	Time        time.Time
	ConnID      uint64
	Protocol    string
	SQL         string
	Fingerprint string
	Verdict     string
	PolicyName  string
	DurationMs  float64
	Rows        int64
	Issues      []string
	Complexity  int
}

type Stats struct {
	Total       int64
	Blocked     int64
	Warned      int64
	Allowed     int64
	ActiveConns int64
}

type Store struct {
	mu      sync.RWMutex
	entries []*QueryEntry
	nextID  atomic.Uint64

	total   atomic.Int64
	blocked atomic.Int64
	warned  atomic.Int64
	allowed atomic.Int64

	// SSE pub/sub
	subsMu  sync.Mutex
	subs    map[uint64]chan *QueryEntry
	subNext uint64
}

func NewStore() *Store {
	return &Store{
		subs: make(map[uint64]chan *QueryEntry),
	}
}

func (s *Store) Add(entry *QueryEntry) {
	entry.ID = s.nextID.Add(1)

	s.mu.Lock()
	s.entries = append(s.entries, entry)
	if len(s.entries) > MaxEntries {
		s.entries = s.entries[1:] // убираем самый старый
	}
	s.mu.Unlock()

	s.total.Add(1)
	switch entry.Verdict {
	case "block":
		s.blocked.Add(1)
	case "warn":
		s.warned.Add(1)
	default:
		s.allowed.Add(1)
	}

	// Рассылаем всем SSE подписчикам
	s.subsMu.Lock()
	for _, ch := range s.subs {
		select {
		case ch <- entry:
		default: // подписчик медленный — пропускаем
		}
	}
	s.subsMu.Unlock()
}

// Recent возвращает последние n записей (новые первые)
func (s *Store) Recent(limit int) []*QueryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := len(s.entries)
	if limit > total {
		limit = total
	}

	result := make([]*QueryEntry, limit)
	for i := 0; i < limit; i++ {
		result[i] = s.entries[total-1-i]
	}
	return result
}

func (s *Store) Stats() Stats {
	return Stats{
		Total:   s.total.Load(),
		Blocked: s.blocked.Load(),
		Warned:  s.warned.Load(),
		Allowed: s.allowed.Load(),
	}
}

func (s *Store) Subscribe() (uint64, chan *QueryEntry) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()

	id := s.subNext
	s.subNext++
	ch := make(chan *QueryEntry, 64)
	s.subs[id] = ch
	return id, ch
}

func (s *Store) Unsubscribe(id uint64) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()

	if ch, ok := s.subs[id]; ok {
		delete(s.subs, id)
		close(ch)
	}
}
