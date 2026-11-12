package hold

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/DeborahPanzer/ledger-vault/pkg/currency"
)

var (
	// ErrInvalidHoldInput indicates invalid identifiers, non-positive amounts, or invalid expiration windows.
	ErrInvalidHoldInput = errors.New("hold: invalid parameters, non-positive amount, or expired window")
	// ErrInsufficientAvailableFunds indicates AvailableMinor (Posted - PendingHold) cannot cover the hold.
	ErrInsufficientAvailableFunds = errors.New("hold: insufficient available balance for authorization hold")
	// ErrHoldNotFound indicates no reservation exists for the given HoldID.
	ErrHoldNotFound = errors.New("hold: reservation not found")
	// ErrHoldIdempotencyConflict indicates a HoldID or CaptureID was reused with mutated parameters.
	ErrHoldIdempotencyConflict = errors.New("hold: idempotency key reused with mutated parameters")
	// ErrHoldInactiveOrExpired indicates capture was attempted against an expired, voided, or closed hold.
	ErrHoldInactiveOrExpired = errors.New("hold: reservation is expired, voided, or already closed")
	// ErrOverCaptureAttempt indicates requested capture exceeds RemainingHoldMinor.
	ErrOverCaptureAttempt = errors.New("hold: capture amount exceeds remaining authorization hold")
)

// Status represents the lifecycle state of a two-stage balance reservation.
type Status string

const (
	StatusActive         Status = "ACTIVE"
	StatusCapturedClosed Status = "CAPTURED_CLOSED"
	StatusVoided         Status = "VOIDED"
	StatusExpired        Status = "EXPIRED"
)

// LayeredBalance exposes the three canonical ledger balance layers for an account.
type LayeredBalance struct {
	AccountCode      string
	Currency         string
	PostedMinor      int64
	PendingHoldMinor int64
	AvailableMinor   int64
}

// Reservation tracks an in-flight authorization or payout hold.
type Reservation struct {
	HoldID             string
	AccountCode        string
	Currency           string
	OriginalHoldMinor  int64
	CapturedMinor      int64
	RemainingHoldMinor int64
	Status             Status
	ExpiresAtUTC       time.Time
}

type accountPosition struct {
	currency    string
	postedMinor int64
	holdMinor   int64
}

type captureReceipt struct {
	holdID      string
	amountMinor int64
	reservation Reservation
}

// LayeredHoldEngine coordinates atomic authorization holds, partial captures, voids, and TTL expirations.
type LayeredHoldEngine struct {
	mu        sync.Mutex
	positions map[string]*accountPosition
	holds     map[string]*Reservation
	captures  map[string]captureReceipt
}

// NewLayeredHoldEngine initializes a three-layer balance reservation coordinator.
func NewLayeredHoldEngine() *LayeredHoldEngine {
	return &LayeredHoldEngine{
		positions: make(map[string]*accountPosition),
		holds:     make(map[string]*Reservation),
		captures:  make(map[string]captureReceipt),
	}
}

// CreditPosted adds settled funds directly to an account's PostedMinor balance.
func (e *LayeredHoldEngine) CreditPosted(accountCode, rawCurrency string, amountMinor int64) (LayeredBalance, error) {
	acct := strings.TrimSpace(accountCode)
	if acct == "" || amountMinor <= 0 {
		return LayeredBalance{}, ErrInvalidHoldInput
	}
	spec, err := currency.Lookup(rawCurrency)
	if err != nil {
		return LayeredBalance{}, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	pos, ok := e.positions[acct]
	if !ok {
		pos = &accountPosition{currency: spec.Code}
		e.positions[acct] = pos
	} else if pos.currency != spec.Code {
		return LayeredBalance{}, ErrInvalidHoldInput
	}

	nextPosted, err := currency.CheckedAdd(pos.postedMinor, amountMinor)
	if err != nil {
		return LayeredBalance{}, err
	}
	pos.postedMinor = nextPosted
	return snapshotPosition(acct, pos), nil
}

// PlaceHold locks amountMinor from AvailableMinor into PendingHoldMinor without mutating PostedMinor.
func (e *LayeredHoldEngine) PlaceHold(holdID, accountCode, rawCurrency string, amountMinor int64, now, expiresAt time.Time) (Reservation, error) {
	hID := strings.TrimSpace(holdID)
	acct := strings.TrimSpace(accountCode)
	if hID == "" || acct == "" || amountMinor <= 0 || !now.Before(expiresAt) {
		return Reservation{}, ErrInvalidHoldInput
	}
	spec, err := currency.Lookup(rawCurrency)
	if err != nil {
		return Reservation{}, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if existing, ok := e.holds[hID]; ok {
		if existing.AccountCode != acct || existing.Currency != spec.Code || existing.OriginalHoldMinor != amountMinor {
			return Reservation{}, fmt.Errorf("%w: holdID=%s", ErrHoldIdempotencyConflict, hID)
		}
		return *existing, nil
	}

	pos, ok := e.positions[acct]
	if !ok || pos.currency != spec.Code {
		return Reservation{}, ErrInsufficientAvailableFunds
	}

	available := pos.postedMinor - pos.holdMinor
	if available < amountMinor {
		return Reservation{}, fmt.Errorf("%w: available=%d requested=%d", ErrInsufficientAvailableFunds, available, amountMinor)
	}

	nextHold, err := currency.CheckedAdd(pos.holdMinor, amountMinor)
	if err != nil {
		return Reservation{}, err
	}
	pos.holdMinor = nextHold

	res := &Reservation{
		HoldID:             hID,
		AccountCode:        acct,
		Currency:           spec.Code,
		OriginalHoldMinor:  amountMinor,
		CapturedMinor:      0,
		RemainingHoldMinor: amountMinor,
		Status:             StatusActive,
		ExpiresAtUTC:       expiresAt.UTC(),
	}
	e.holds[hID] = res
	return *res, nil
}

// CaptureHold converts up to RemainingHoldMinor into a final debit from PostedMinor, optionally releasing unused surplus.
func (e *LayeredHoldEngine) CaptureHold(holdID, captureID string, captureAmountMinor int64, finalClose bool, now time.Time) (Reservation, error) {
	hID := strings.TrimSpace(holdID)
	cID := strings.TrimSpace(captureID)
	if hID == "" || cID == "" || captureAmountMinor <= 0 {
		return Reservation{}, ErrInvalidHoldInput
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if cached, exists := e.captures[cID]; exists {
		if cached.holdID != hID || cached.amountMinor != captureAmountMinor {
			return Reservation{}, fmt.Errorf("%w: captureID=%s", ErrHoldIdempotencyConflict, cID)
		}
		return cached.reservation, nil
	}

	res, ok := e.holds[hID]
	if !ok {
		return Reservation{}, ErrHoldNotFound
	}
	pos := e.positions[res.AccountCode]

	// Atomic TTL check: if the hold expired before capture arrival, release remaining hold and reject late capture.
	if res.Status == StatusActive && !now.Before(res.ExpiresAtUTC) {
		pos.holdMinor -= res.RemainingHoldMinor
		res.RemainingHoldMinor = 0
		res.Status = StatusExpired
		return Reservation{}, ErrHoldInactiveOrExpired
	}
	if res.Status != StatusActive {
		return Reservation{}, ErrHoldInactiveOrExpired
	}
	if captureAmountMinor > res.RemainingHoldMinor {
		return Reservation{}, fmt.Errorf("%w: remaining=%d capture=%d", ErrOverCaptureAttempt, res.RemainingHoldMinor, captureAmountMinor)
	}

	pos.postedMinor -= captureAmountMinor
	pos.holdMinor -= captureAmountMinor
	res.CapturedMinor += captureAmountMinor
	res.RemainingHoldMinor -= captureAmountMinor

	if finalClose || res.RemainingHoldMinor == 0 {
		if res.RemainingHoldMinor > 0 {
			pos.holdMinor -= res.RemainingHoldMinor
			res.RemainingHoldMinor = 0
		}
		res.Status = StatusCapturedClosed
	}

	snap := *res
	e.captures[cID] = captureReceipt{
		holdID:      hID,
		amountMinor: captureAmountMinor,
		reservation: snap,
	}
	return snap, nil
}

// VoidHold releases any uncaptured RemainingHoldMinor back into AvailableMinor.
func (e *LayeredHoldEngine) VoidHold(holdID string) (Reservation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	res, ok := e.holds[strings.TrimSpace(holdID)]
	if !ok {
		return Reservation{}, ErrHoldNotFound
	}
	if res.Status == StatusVoided {
		return *res, nil
	}
	if res.Status != StatusActive {
		return Reservation{}, ErrHoldInactiveOrExpired
	}

	pos := e.positions[res.AccountCode]
	pos.holdMinor -= res.RemainingHoldMinor
	res.RemainingHoldMinor = 0
	res.Status = StatusVoided
	return *res, nil
}

// InspectBalance returns the current Posted, PendingHold, and Available balance layers.
func (e *LayeredHoldEngine) InspectBalance(accountCode string) (LayeredBalance, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	acct := strings.TrimSpace(accountCode)
	pos, ok := e.positions[acct]
	if !ok {
		return LayeredBalance{}, ErrHoldNotFound
	}
	return snapshotPosition(acct, pos), nil
}

func snapshotPosition(acct string, pos *accountPosition) LayeredBalance {
	return LayeredBalance{
		AccountCode:      acct,
		Currency:         pos.currency,
		PostedMinor:      pos.postedMinor,
		PendingHoldMinor: pos.holdMinor,
		AvailableMinor:   pos.postedMinor - pos.holdMinor,
	}
}
