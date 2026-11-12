package hold

import (
	"errors"
	"testing"
	"time"
)

func TestLayeredHoldEngine_PartialCaptureSurplusReleaseAndExpiry(t *testing.T) {
	eng := NewLayeredHoldEngine()
	now := time.Date(2026, time.November, 12, 12, 0, 0, 0, time.UTC)

	// Seed $1,000.00 (100,000 cents) into merchant payout pool
	bal, err := eng.CreditPosted("LIABILITY:MERCHANT_POOL_USD", "USD", 100_000)
	if err != nil || bal.AvailableMinor != 100_000 {
		t.Fatalf("unexpected seed result: %+v (%v)", bal, err)
	}

	// Place $400.00 hold -> Posted remains 100,000, PendingHold = 40,000, Available = 60,000
	res, err := eng.PlaceHold("hold_01", "LIABILITY:MERCHANT_POOL_USD", "USD", 40_000, now, now.Add(15*time.Minute))
	if err != nil || res.RemainingHoldMinor != 40_000 {
		t.Fatalf("unexpected PlaceHold result: %+v (%v)", res, err)
	}

	bal, _ = eng.InspectBalance("LIABILITY:MERCHANT_POOL_USD")
	if bal.PostedMinor != 100_000 || bal.PendingHoldMinor != 40_000 || bal.AvailableMinor != 60_000 {
		t.Fatalf("unexpected 3-layer split after hold: %+v", bal)
	}

	// Partial final capture of $350.00 (finalClose = true) -> Deducts $350 from Posted, releases $50 surplus back to Available!
	closed, err := eng.CaptureHold("hold_01", "cap_01", 35_000, true, now.Add(5*time.Minute))
	if err != nil || closed.Status != StatusCapturedClosed || closed.RemainingHoldMinor != 0 {
		t.Fatalf("expected closed capture releasing surplus, got %+v (%v)", closed, err)
	}

	bal, _ = eng.InspectBalance("LIABILITY:MERCHANT_POOL_USD")
	if bal.PostedMinor != 65_000 || bal.PendingHoldMinor != 0 || bal.AvailableMinor != 65_000 {
		t.Fatalf("expected Posted=65000, Hold=0, Available=65000, got %+v", bal)
	}

	// Second hold that expires before capture arrives -> Late capture rejected & hold auto-released
	_, _ = eng.PlaceHold("hold_exp", "LIABILITY:MERCHANT_POOL_USD", "USD", 20_000, now, now.Add(10*time.Minute))
	_, expErr := eng.CaptureHold("hold_exp", "cap_late", 20_000, true, now.Add(11*time.Minute))
	if !errors.Is(expErr, ErrHoldInactiveOrExpired) {
		t.Fatalf("expected ErrHoldInactiveOrExpired on late capture, got %v", expErr)
	}

	balAfterExp, _ := eng.InspectBalance("LIABILITY:MERCHANT_POOL_USD")
	if balAfterExp.PendingHoldMinor != 0 || balAfterExp.AvailableMinor != 65_000 {
		t.Fatalf("expected expired hold to release PendingHoldMinor back to AvailableMinor, got %+v", balAfterExp)
	}
}
