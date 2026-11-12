package journal

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DeborahPanzer/ledger-vault/pkg/coa"
	"github.com/DeborahPanzer/ledger-vault/pkg/currency"
)

var (
	// ErrInvalidEntryMetadata indicates missing entry IDs, idempotency keys, or fewer than 2 posting legs.
	ErrInvalidEntryMetadata = errors.New("journal: entry requires non-empty ID, idempotency key, and at least 2 legs")
	// ErrInvalidPostingLeg indicates a non-positive leg amount, blank account code, or invalid side.
	ErrInvalidPostingLeg = errors.New("journal: posting leg requires positive amount, account code, and DEBIT/CREDIT side")
	// ErrUnbalancedJournalEntry indicates total debits do not equal total credits for a currency.
	ErrUnbalancedJournalEntry = errors.New("journal: zero-sum double-entry invariant violated (debits != credits)")
	// ErrSelfTransferLoop indicates all debit and credit legs within a currency hit the exact same account.
	ErrSelfTransferLoop = errors.New("journal: self-referencing single-account transfer is prohibited")
)

// Leg represents an individual immutable debit or credit line item inside a journal entry.
type Leg struct {
	AccountCode string
	Side        coa.EntrySide
	Currency    string
	AmountMinor int64
}

// Entry represents a validated, zero-sum balanced double-entry journal transaction.
type Entry struct {
	EntryID           string
	IdempotencyKey    string
	CorrelationID     string
	CausationID       string
	ReversalOfEntryID string
	Description       string
	PostedAtUTC       time.Time
	Legs              []Leg
}

// NewEntry validates strict per-currency zero-sum balance conservation (Sum(Debits) == Sum(Credits)).
func NewEntry(
	entryID, idempotencyKey, correlationID, causationID, description string,
	postedAt time.Time,
	rawLegs []Leg,
) (Entry, error) {
	eID := strings.TrimSpace(entryID)
	idem := strings.TrimSpace(idempotencyKey)
	if eID == "" || idem == "" || len(rawLegs) < 2 {
		return Entry{}, ErrInvalidEntryMetadata
	}

	type ccyTally struct {
		debits   int64
		credits  int64
		accounts map[string]struct{}
	}
	tallies := make(map[string]*ccyTally)
	cleanLegs := make([]Leg, 0, len(rawLegs))

	for _, leg := range rawLegs {
		acct := strings.TrimSpace(leg.AccountCode)
		if acct == "" || leg.AmountMinor <= 0 || (leg.Side != coa.SideDebit && leg.Side != coa.SideCredit) {
			return Entry{}, ErrInvalidPostingLeg
		}
		spec, err := currency.Lookup(leg.Currency)
		if err != nil {
			return Entry{}, err
		}

		tally, exists := tallies[spec.Code]
		if !exists {
			tally = &ccyTally{accounts: make(map[string]struct{})}
			tallies[spec.Code] = tally
		}
		tally.accounts[acct] = struct{}{}

		if leg.Side == coa.SideDebit {
			next, err := currency.CheckedAdd(tally.debits, leg.AmountMinor)
			if err != nil {
				return Entry{}, err
			}
			tally.debits = next
		} else {
			next, err := currency.CheckedAdd(tally.credits, leg.AmountMinor)
			if err != nil {
				return Entry{}, err
			}
			tally.credits = next
		}

		cleanLegs = append(cleanLegs, Leg{
			AccountCode: acct,
			Side:        leg.Side,
			Currency:    spec.Code,
			AmountMinor: leg.AmountMinor,
		})
	}

	for ccy, tally := range tallies {
		if tally.debits != tally.credits {
			return Entry{}, fmt.Errorf(
				"%w: currency=%s debits=%d credits=%d",
				ErrUnbalancedJournalEntry,
				ccy,
				tally.debits,
				tally.credits,
			)
		}
		if len(tally.accounts) < 2 {
			return Entry{}, fmt.Errorf("%w: currency=%s", ErrSelfTransferLoop, ccy)
		}
	}

	return Entry{
		EntryID:        eID,
		IdempotencyKey: idem,
		CorrelationID:  strings.TrimSpace(correlationID),
		CausationID:    strings.TrimSpace(causationID),
		Description:    strings.TrimSpace(description),
		PostedAtUTC:    postedAt.UTC(),
		Legs:           cleanLegs,
	}, nil
}

// BuildReversingEntry constructs an append-only compensating entry by inverting DEBIT <-> CREDIT on every leg.
func BuildReversingEntry(original Entry, reversalEntryID, reversalIdempotencyKey, reason string, now time.Time) (Entry, error) {
	inverted := make([]Leg, len(original.Legs))
	for i, leg := range original.Legs {
		flippedSide := coa.SideDebit
		if leg.Side == coa.SideDebit {
			flippedSide = coa.SideCredit
		}
		inverted[i] = Leg{
			AccountCode: leg.AccountCode,
			Side:        flippedSide,
			Currency:    leg.Currency,
			AmountMinor: leg.AmountMinor,
		}
	}

	rev, err := NewEntry(
		reversalEntryID,
		reversalIdempotencyKey,
		original.CorrelationID,
		original.EntryID,
		strings.TrimSpace(reason),
		now,
		inverted,
	)
	if err != nil {
		return Entry{}, err
	}
	rev.ReversalOfEntryID = original.EntryID
	return rev, nil
}
