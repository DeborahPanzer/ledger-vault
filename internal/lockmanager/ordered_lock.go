package lockmanager

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// ErrEmptyAccountLockSet indicates an empty or blank account slice was passed to AcquireAll.
var ErrEmptyAccountLockSet = errors.New("lockmanager: account lock set cannot be empty")

type accountSlot struct {
	mu sync.Mutex
}

// LockGuard represents an acquired multi-account exclusive lock grant with a monotonic fencing epoch.
type LockGuard struct {
	FencingEpoch    uint64
	OrderedAccounts []string
	slots           []*accountSlot
	releaseOnce     sync.Once
}

// Release unlocks all held account mutexes in strict reverse acquisition order.
func (g *LockGuard) Release() {
	if g == nil {
		return
	}
	g.releaseOnce.Do(func() {
		for i := len(g.slots) - 1; i >= 0; i-- {
			g.slots[i].mu.Unlock()
		}
	})
}

// OrderedLockManager acquires multi-account locks in canonical lexicographical order to eliminate AB-BA deadlocks.
type OrderedLockManager struct {
	regMu       sync.Mutex
	epochSeq    atomic.Uint64
	accountMap  map[string]*accountSlot
}

// NewOrderedLockManager creates a deadlock-free lexicographical account lock coordinator.
func NewOrderedLockManager() *OrderedLockManager {
	return &OrderedLockManager{
		accountMap: make(map[string]*accountSlot),
	}
}

// AcquireAll sorts, deduplicates, and exclusively locks all target accounts in ascending ASCII order.
func (m *OrderedLockManager) AcquireAll(rawAccounts []string) (*LockGuard, error) {
	normalized := make([]string, 0, len(rawAccounts))
	for _, raw := range rawAccounts {
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	if len(normalized) == 0 {
		return nil, ErrEmptyAccountLockSet
	}

	sort.Strings(normalized)

	// Deduplicate sorted slice in-place so repeated account references never self-deadlock.
	unique := normalized[:0]
	for i, code := range normalized {
		if i == 0 || code != normalized[i-1] {
			unique = append(unique, code)
		}
	}

	slots := make([]*accountSlot, len(unique))
	m.regMu.Lock()
	for i, code := range unique {
		slot, exists := m.accountMap[code]
		if !exists {
			slot = &accountSlot{}
			m.accountMap[code] = slot
		}
		slots[i] = slot
	}
	m.regMu.Unlock()

	for _, slot := range slots {
		slot.mu.Lock()
	}

	return &LockGuard{
		FencingEpoch:    m.epochSeq.Add(1),
		OrderedAccounts: unique,
		slots:           slots,
	}, nil
}
