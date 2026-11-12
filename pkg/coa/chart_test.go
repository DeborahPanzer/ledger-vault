package coa

import (
	"errors"
	"testing"
)

func TestChartOfAccounts_NormalSignOverdraftAndComplianceFreeze(t *testing.T) {
	reg := NewRegistry()

	// Merchant Payable is a CREDIT-normal LIABILITY with overdraft disabled.
	_, err := reg.Register("LIABILITY:MERCHANT_01_USD", "Merchant 01 Payable", CategoryLiability, "USD", false)
	if err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	// Credit $500.00 increases Liability normal balance to +50,000 minor units.
	snap, err := reg.ApplyPosting("LIABILITY:MERCHANT_01_USD", SideCredit, "USD", 50_000)
	if err != nil || snap.BalanceMinor != 50_000 {
		t.Fatalf("expected +50000 balance after credit, got %+v (err=%v)", snap, err)
	}

	// Place compliance freeze: outbound payout DEBIT (delta < 0) must be blocked, inbound recovery CREDIT (delta > 0) allowed.
	if err := reg.SetStatus("LIABILITY:MERCHANT_01_USD", StatusFrozenCompliance); err != nil {
		t.Fatalf("unexpected status transition error: %v", err)
	}

	if _, err := reg.ApplyPosting("LIABILITY:MERCHANT_01_USD", SideDebit, "USD", 10_000); !errors.Is(err, ErrAccountComplianceFrozen) {
		t.Fatalf("expected ErrAccountComplianceFrozen on outbound debit, got %v", err)
	}

	snap, err = reg.ApplyPosting("LIABILITY:MERCHANT_01_USD", SideCredit, "USD", 5_000)
	if err != nil || snap.BalanceMinor != 55_000 {
		t.Fatalf("expected inbound settlement credit during freeze to succeed (55000), got %+v (err=%v)", snap, err)
	}

	// Restore active status and verify overdraft protection blocks $600.00 debit against $550.00 balance.
	_ = reg.SetStatus("LIABILITY:MERCHANT_01_USD", StatusActive)
	if _, err := reg.ApplyPosting("LIABILITY:MERCHANT_01_USD", SideDebit, "USD", 60_000); !errors.Is(err, ErrInsufficientLedgerFunds) {
		t.Fatalf("expected ErrInsufficientLedgerFunds on over-debit, got %v", err)
	}

	// Currency mismatch must fail strictly.
	if _, err := reg.ApplyPosting("LIABILITY:MERCHANT_01_USD", SideCredit, "EUR", 1_000); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("expected ErrCurrencyMismatch for EUR posting on USD account, got %v", err)
	}
}
