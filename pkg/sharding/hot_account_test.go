package sharding

import (
	"fmt"
	"sync"
	"testing"
)

func TestHotAccountRouter_HighConcurrencyShardingAndConsolidation(t *testing.T) {
	router, err := NewHotAccountRouter(32)
	if err != nil {
		t.Fatalf("unexpected router init error: %v", err)
	}

	const writers = 500
	const feePerTx = int64(290) // $2.90 platform fee per transaction
	var wg sync.WaitGroup

	// Run concurrent writers alongside concurrent consolidated snapshot readers under -race
	for i := 0; i < writers; i++ {
		wg.Add(1)
		txKey := fmt.Sprintf("pi_tx_%05d", i)
		go func(k string, idx int) {
			defer wg.Done()
			if _, _, err := router.PostByPartitionKey("REVENUE:PLATFORM_FEE_USD", "USD", k, feePerTx); err != nil {
				t.Errorf("unexpected shard post error: %v", err)
			}
			if idx%25 == 0 {
				if _, err := router.ConsolidatedBalance("REVENUE:PLATFORM_FEE_USD", "USD"); err != nil {
					t.Errorf("unexpected concurrent snapshot error: %v", err)
				}
			}
		}(txKey, i)
	}
	wg.Wait()

	snap, err := router.ConsolidatedBalance("REVENUE:PLATFORM_FEE_USD", "USD")
	if err != nil {
		t.Fatalf("unexpected final snapshot error: %v", err)
	}

	expectedTotal := int64(writers) * feePerTx
	if snap.TotalBalanceMinor != expectedTotal {
		t.Fatalf("consolidated balance = %d, want %d", snap.TotalBalanceMinor, expectedTotal)
	}
	if snap.NonZeroShards < 16 {
		t.Fatalf("expected wide FNV-1a distribution across >=16 of 32 shards, got %d", snap.NonZeroShards)
	}
}
