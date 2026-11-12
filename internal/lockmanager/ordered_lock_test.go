package lockmanager

import (
	"sync"
	"testing"
)

func TestOrderedLockManager_BidirectionalRingTransfersZeroDeadlock(t *testing.T) {
	mgr := NewOrderedLockManager()
	balances := map[string]int64{
		"ASSET:ACCT_A": 1_000_000,
		"ASSET:ACCT_B": 1_000_000,
		"ASSET:ACCT_C": 1_000_000,
	}

	pairs := [][2]string{
		{"ASSET:ACCT_A", "ASSET:ACCT_B"},
		{"ASSET:ACCT_B", "ASSET:ACCT_A"}, // Direct opposite order (classic AB-BA deadlock trigger)
		{"ASSET:ACCT_B", "ASSET:ACCT_C"},
		{"ASSET:ACCT_C", "ASSET:ACCT_A"},
	}

	const goroutines = 300
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		pair := pairs[i%len(pairs)]
		go func(src, dst string) {
			defer wg.Done()
			// Pass duplicate reference intentionally to verify deduplication prevents self-deadlock
			guard, err := mgr.AcquireAll([]string{dst, src, dst})
			if err != nil {
				t.Errorf("unexpected AcquireAll error: %v", err)
				return
			}
			defer guard.Release()

			balances[src] -= 100
			balances[dst] += 100
		}(pair[0], pair[1])
	}
	wg.Wait()

	total := balances["ASSET:ACCT_A"] + balances["ASSET:ACCT_B"] + balances["ASSET:ACCT_C"]
	if total != 3_000_000 {
		t.Fatalf("conservation violated under concurrent locking: got %d, want 3000000", total)
	}
}
