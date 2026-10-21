package ledger

import (
	"testing"
	"time"
)

func TestValidateAndSealTransaction_Balanced(t *testing.T) {
	tx := &JournalTransaction{
		TransactionID:  "tx_01HV8X9Z",
		IdempotencyKey: "idem_99887766",
		Currency:       "USD",
		CreatedAt:      time.Now().UTC(),
		PreviousHash:   "0000000000000000000000000000000000000000000000000000000000000000",
		Lines: []LedgerLine{
			{AccountID: "acct_platform_float", Direction: Debit, Amount: 15000},
			{AccountID: "acct_merchant_escrow", Direction: Credit, Amount: 14550},
			{AccountID: "acct_platform_revenue", Direction: Credit, Amount: 450},
		},
	}

	err := ValidateAndSealTransaction(tx)
	if err != nil {
		t.Fatalf("expected balanced transaction to succeed, got error: %v", err)
	}

	if len(tx.EntryHash) != 64 {
		t.Errorf("expected 64-character SHA-256 hex digest, got length %d", len(tx.EntryHash))
	}
}

func TestValidateAndSealTransaction_UnbalancedRejected(t *testing.T) {
	tx := &JournalTransaction{
		TransactionID:  "tx_01HV8XAA",
		IdempotencyKey: "idem_99887767",
		Currency:       "USD",
		CreatedAt:      time.Now().UTC(),
		PreviousHash:   "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Lines: []LedgerLine{
			{AccountID: "acct_platform_float", Direction: Debit, Amount: 10000},
			{AccountID: "acct_merchant_escrow", Direction: Credit, Amount: 9999}, // Off by 1 cent
		},
	}

	err := ValidateAndSealTransaction(tx)
	if err == nil {
		t.Fatal("expected unbalanced transaction (off by 1 minor unit) to be rejected, but it succeeded")
	}
}

func TestValidateAndSealTransaction_NonPositiveAmountRejected(t *testing.T) {
	tx := &JournalTransaction{
		TransactionID:  "tx_01HV8XBB",
		IdempotencyKey: "idem_99887768",
		Currency:       "USD",
		Lines: []LedgerLine{
			{AccountID: "acct_platform_float", Direction: Debit, Amount: -500},
			{AccountID: "acct_merchant_escrow", Direction: Credit, Amount: -500},
		},
	}

	if err := ValidateAndSealTransaction(tx); err == nil {
		t.Fatal("expected negative minor unit amount to be rejected")
	}
}
