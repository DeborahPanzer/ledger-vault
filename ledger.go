package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type EntryDirection string

const (
	Debit  EntryDirection = "DEBIT"
	Credit EntryDirection = "CREDIT"
)

type LedgerLine struct {
	AccountID string
	Direction EntryDirection
	Amount    int64
}

type JournalTransaction struct {
	TransactionID  string
	IdempotencyKey string
	Currency       string
	Lines          []LedgerLine
	CreatedAt      time.Time
	PreviousHash   string
	EntryHash      string
}

// ValidateAndSealTransaction enforces zero-sum double-entry invariants and computes SHA-256 audit hash.
func ValidateAndSealTransaction(tx *JournalTransaction) error {
	if len(tx.Lines) < 2 {
		return errors.New("ledger-vault: double-entry transaction requires at least two lines")
	}

	var totalDebit, totalCredit int64
	for _, line := range tx.Lines {
		if line.Amount <= 0 {
			return errors.New("ledger-vault: line amount must be strictly positive integer")
		}
		switch line.Direction {
		case Debit:
			totalDebit += line.Amount
		case Credit:
			totalCredit += line.Amount
		default:
			return errors.New("ledger-vault: invalid entry direction")
		}
	}

	if totalDebit != totalCredit {
		return fmt.Errorf("ledger-vault: unbalanced journal entry (debit=%d, credit=%d)", totalDebit, totalCredit)
	}

	payload := fmt.Sprintf("%s:%s:%s:%d:%s", tx.TransactionID, tx.IdempotencyKey, tx.Currency, totalDebit, tx.PreviousHash)
	hash := sha256.Sum256([]byte(payload))
	tx.EntryHash = hex.EncodeToString(hash[:])

	return nil
}
