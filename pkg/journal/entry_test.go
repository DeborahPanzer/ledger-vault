package journal

import (
	"errors"
	"testing"
	"time"

	"github.com/DeborahPanzer/ledger-vault/pkg/coa"
)

func TestNewEntry_MarketplaceSplitAndReversalSymmetry(t *testing.T) {
	now := time.Date(2026, time.November, 12, 16, 0, 0, 0, time.UTC)

	// 3-leg Marketplace Capture: $100.00 Acquirer Clearing -> $97.10 Merchant Payable + $2.90 Platform Fee
	splitLegs := []Leg{
		{AccountCode: "ASSET:ACQUIRER_CLEARING_USD", Side: coa.SideDebit, Currency: "USD", AmountMinor: 10_000},
		{AccountCode: "LIABILITY:MERCHANT_PAYABLE_USD", Side: coa.SideCredit, Currency: "USD", AmountMinor: 9_710},
		{AccountCode: "REVENUE:PLATFORM_FEE_USD", Side: coa.SideCredit, Currency: "USD", AmountMinor: 290},
	}

	entry, err := NewEntry("jrn_001", "idem_001", "corr_001", "pi_001", "Marketplace capture", now, splitLegs)
	if err != nil {
		t.Fatalf("expected balanced 3-leg entry to succeed, got: %v", err)
	}

	// Build append-only reversal and verify net effect per account is exactly 0.
	rev, err := BuildReversingEntry(entry, "jrn_001_rev", "idem_001_rev", "Full refund reversal", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("expected BuildReversingEntry to succeed, got: %v", err)
	}
	if rev.ReversalOfEntryID != "jrn_001" || rev.Legs[0].Side != coa.SideCredit {
		t.Fatalf("unexpected reversal metadata or leg inversion: %+v", rev)
	}

	// Off-by-one penny imbalance ($100.00 debit vs $99.99 credit) MUST fail.
	unbalanced := []Leg{
		{AccountCode: "ASSET:ACQUIRER_CLEARING_USD", Side: coa.SideDebit, Currency: "USD", AmountMinor: 10_000},
		{AccountCode: "LIABILITY:MERCHANT_PAYABLE_USD", Side: coa.SideCredit, Currency: "USD", AmountMinor: 9_999},
	}
	if _, err := NewEntry("jrn_bad", "idem_bad", "c", "p", "Off by 1 cent", now, unbalanced); !errors.Is(err, ErrUnbalancedJournalEntry) {
		t.Fatalf("expected ErrUnbalancedJournalEntry on 1-cent discrepancy, got: %v", err)
	}

	// Self-transfer loop (same account debited and credited) MUST fail.
	selfLoop := []Leg{
		{AccountCode: "ASSET:VAULT_USD", Side: coa.SideDebit, Currency: "USD", AmountMinor: 5_000},
		{AccountCode: "ASSET:VAULT_USD", Side: coa.SideCredit, Currency: "USD", AmountMinor: 5_000},
	}
	if _, err := NewEntry("jrn_loop", "idem_loop", "c", "p", "Self loop", now, selfLoop); !errors.Is(err, ErrSelfTransferLoop) {
		t.Fatalf("expected ErrSelfTransferLoop, got: %v", err)
	}
}
