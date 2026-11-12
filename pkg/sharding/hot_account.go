package sharding

import (
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/DeborahPanzer/ledger-vault/pkg/currency"
)

var (
	// ErrInvalidShardCount indicates shardCount is outside [1, 256].
	ErrInvalidShardCount = errors.New("sharding: shard count must be between 1 and 256")
	// ErrInvalidShardPosting indicates empty logical account or invalid ISO-4217 currency.
	ErrInvalidShardPosting = errors.New("sharding: invalid logical account code or currency")
)

type shardBucket struct {
	mu       sync.RWMutex
	balances map[string]int64
}

// ConsolidatedSnapshot represents a linearizable point-in-time balance sum across all virtual shards.
type ConsolidatedSnapshot struct {
	LogicalAccountCode string
	Currency           string
	ShardCount         uint32
	NonZeroShards      int
	TotalBalanceMinor  int64
}

// HotAccountRouter distributes high-frequency clearing and fee postings across N isolated lock shards.
type HotAccountRouter struct {
	shardCount uint32
	rrCounter  atomic.Uint64
	shards     []shardBucket
}

// NewHotAccountRouter initializes N isolated sub-account buckets (recommended 16..64 for hot clearing accounts).
func NewHotAccountRouter(shardCount uint32) (*HotAccountRouter, error) {
	if shardCount < 1 || shardCount > 256 {
		return nil, ErrInvalidShardCount
	}
	buckets := make([]shardBucket, shardCount)
	for i := range buckets {
		buckets[i] = shardBucket{balances: make(map[string]int64)}
	}
	return &HotAccountRouter{
		shardCount: shardCount,
		shards:     buckets,
	}, nil
}

func compositeKey(logicalCode, ccy string) string {
	return logicalCode + "|" + ccy
}

// PhysicalShardCode formats the deterministic sub-account identifier persisted in SQL postings.
func PhysicalShardCode(logicalCode string, shardIdx uint32) string {
	return fmt.Sprintf("%s#shard_%02d", logicalCode, shardIdx)
}

// PostByPartitionKey routes postings deterministically via FNV-1a (e.g., by paymentIntentId or merchantId).
func (r *HotAccountRouter) PostByPartitionKey(logicalCode, rawCurrency, partitionKey string, deltaMinor int64) (string, int64, error) {
	cleanCode := strings.TrimSpace(logicalCode)
	if cleanCode == "" {
		return "", 0, ErrInvalidShardPosting
	}
	spec, err := currency.Lookup(rawCurrency)
	if err != nil {
		return "", 0, err
	}

	var idx uint32
	if trimmedKey := strings.TrimSpace(partitionKey); trimmedKey != "" {
		h := fnv.New32a()
		_, _ = h.Write([]byte(trimmedKey))
		idx = h.Sum32() % r.shardCount
	} else {
		idx = uint32((r.rrCounter.Add(1) - 1) % uint64(r.shardCount))
	}

	bucket := &r.shards[idx]
	key := compositeKey(cleanCode, spec.Code)

	bucket.mu.Lock()
	next, err := currency.CheckedAdd(bucket.balances[key], deltaMinor)
	if err != nil {
		bucket.mu.Unlock()
		return "", 0, err
	}
	bucket.balances[key] = next
	bucket.mu.Unlock()

	return PhysicalShardCode(cleanCode, idx), next, nil
}

// ConsolidatedBalance acquires ordered read locks across all shards to compute a linearizable total balance.
func (r *HotAccountRouter) ConsolidatedBalance(logicalCode, rawCurrency string) (ConsolidatedSnapshot, error) {
	cleanCode := strings.TrimSpace(logicalCode)
	if cleanCode == "" {
		return ConsolidatedSnapshot{}, ErrInvalidShardPosting
	}
	spec, err := currency.Lookup(rawCurrency)
	if err != nil {
		return ConsolidatedSnapshot{}, err
	}

	// Acquire RLock in ascending shard index order to guarantee deadlock-free linearizable point-in-time read.
	for i := uint32(0); i < r.shardCount; i++ {
		r.shards[i].mu.RLock()
	}
	defer func() {
		for i := int(r.shardCount) - 1; i >= 0; i-- {
			r.shards[i].mu.RUnlock()
		}
	}()

	key := compositeKey(cleanCode, spec.Code)
	var total int64
	nonZero := 0
	for i := uint32(0); i < r.shardCount; i++ {
		v := r.shards[i].balances[key]
		if v != 0 {
			nonZero++
			total, err = currency.CheckedAdd(total, v)
			if err != nil {
				return ConsolidatedSnapshot{}, err
			}
		}
	}

	return ConsolidatedSnapshot{
		LogicalAccountCode: cleanCode,
		Currency:           spec.Code,
		ShardCount:         r.shardCount,
		NonZeroShards:      nonZero,
		TotalBalanceMinor:  total,
	}, nil
}
