package coa

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/DeborahPanzer/ledger-vault/pkg/currency"
)

var (
	// ErrInvalidAccountDefinition indicates malformed account metadata or unsupported GAAP category.
	ErrInvalidAccountDefinition = errors.New("coa: invalid account code, category, or currency")
	// ErrDuplicateAccountCode indicates an account with the same code is already registered.
	ErrDuplicateAccountCode = errors.New("coa: account code already exists in Chart of Accounts")
	// ErrAccountNotFound indicates the referenced account code is unregistered.
	ErrAccountNotFound = errors.New("coa: account not found in Chart of Accounts")
	// ErrCurrencyMismatch indicates a posting currency differs from the account's immutable ISO-4217 currency.
	ErrCurrencyMismatch = errors.New("coa: posting currency does not match account currency")
	// ErrAccountClosed indicates postings were attempted against a permanently closed ledger account.
	ErrAccountClosed = errors.New("coa: account is permanently closed")
	// ErrAccountComplianceFrozen indicates an outbound balance-decreasing posting hit a regulatory legal hold.
	ErrAccountComplianceFrozen = errors.New("coa: outbound balance reduction blocked by compliance legal hold")
	// ErrInsufficientLedgerFunds indicates a posting would breach the non-negative balance invariant.
	ErrInsufficientLedgerFunds = errors.New("coa: insufficient funds on overdraft-protected account")
)

// Category represents one of the five canonical GAAP double-entry account classifications.
type Category string

const (
	CategoryAsset     Category = "ASSET"
	CategoryLiability Category = "LIABILITY"
	CategoryEquity    Category = "EQUITY"
	CategoryRevenue   Category = "REVENUE"
	CategoryExpense   Category = "EXPENSE"
)

// EntrySide specifies whether a posting leg is a DEBIT or CREDIT.
type EntrySide string

const (
	SideDebit  EntrySide = "DEBIT"
	SideCredit EntrySide = "CREDIT"
)

// AccountStatus governs operational posting eligibility on an account.
type AccountStatus string

const (
	StatusActive           AccountStatus = "ACTIVE"
	StatusFrozenCompliance AccountStatus = "FROZEN_COMPLIANCE"
	StatusClosed           AccountStatus = "CLOSED"
)

// Account represents an immutable metadata specification and current posted balance in the Chart of Accounts.
type Account struct {
	Code           string
	Name           string
	Category       Category
	NormalSide     EntrySide
	Currency       string
	AllowOverdraft bool
	Status         AccountStatus
	BalanceMinor   int64
}

// NormalSideForCategory returns the GAAP normal balance side (DEBIT for Asset/Expense; CREDIT for Liability/Equity/Revenue).
func NormalSideForCategory(cat Category) (EntrySide, error) {
	switch cat {
	case CategoryAsset, CategoryExpense:
		return SideDebit, nil
	case CategoryLiability, CategoryEquity, CategoryRevenue:
		return SideCredit, nil
	default:
		return "", fmt.Errorf("%w: unknown category %q", ErrInvalidAccountDefinition, cat)
	}
}

// ComputeBalanceDelta converts a positive posting amount into a signed normal-balance delta.
func ComputeBalanceDelta(cat Category, side EntrySide, amountMinor int64) (int64, error) {
	if amountMinor <= 0 || (side != SideDebit && side != SideCredit) {
		return 0, ErrInvalidAccountDefinition
	}
	normal, err := NormalSideForCategory(cat)
	if err != nil {
		return 0, err
	}
	if side == normal {
		return amountMinor, nil
	}
	return -amountMinor, nil
}

// Registry manages the thread-safe Chart of Accounts and enforces overdraft and compliance-freeze invariants.
type Registry struct {
	mu       sync.RWMutex
	accounts map[string]*Account
}

// NewRegistry creates an empty GAAP Chart of Accounts registry.
func NewRegistry() *Registry {
	return &Registry{
		accounts: make(map[string]*Account),
	}
}

// Register creates a new ledger account with zero initial balance.
func (r *Registry) Register(code, name string, cat Category, rawCurrency string, allowOverdraft bool) (Account, error) {
	cleanCode := strings.TrimSpace(code)
	cleanName := strings.TrimSpace(name)
	if cleanCode == "" || cleanName == "" {
		return Account{}, ErrInvalidAccountDefinition
	}

	normal, err := NormalSideForCategory(cat)
	if err != nil {
		return Account{}, err
	}
	currSpec, err := currency.Lookup(rawCurrency)
	if err != nil {
		return Account{}, fmt.Errorf("%w: %v", ErrInvalidAccountDefinition, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.accounts[cleanCode]; exists {
		return Account{}, fmt.Errorf("%w: %s", ErrDuplicateAccountCode, cleanCode)
	}

	acct := &Account{
		Code:           cleanCode,
		Name:           cleanName,
		Category:       cat,
		NormalSide:     normal,
		Currency:       currSpec.Code,
		AllowOverdraft: allowOverdraft,
		Status:         StatusActive,
		BalanceMinor:   0,
	}
	r.accounts[cleanCode] = acct
	return *acct, nil
}

// SetStatus transitions an account between ACTIVE, FROZEN_COMPLIANCE, and CLOSED states.
func (r *Registry) SetStatus(code string, nextStatus AccountStatus) error {
	if nextStatus != StatusActive && nextStatus != StatusFrozenCompliance && nextStatus != StatusClosed {
		return ErrInvalidAccountDefinition
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	acct, ok := r.accounts[strings.TrimSpace(code)]
	if !ok {
		return ErrAccountNotFound
	}
	acct.Status = nextStatus
	return nil
}

// ApplyPosting validates currency, compliance-freeze rules, and overdraft protection before updating the balance.
func (r *Registry) ApplyPosting(code string, side EntrySide, rawCurrency string, amountMinor int64) (Account, error) {
	currSpec, err := currency.Lookup(rawCurrency)
	if err != nil {
		return Account{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	acct, ok := r.accounts[strings.TrimSpace(code)]
	if !ok {
		return Account{}, fmt.Errorf("%w: %s", ErrAccountNotFound, code)
	}
	if acct.Currency != currSpec.Code {
		return Account{}, fmt.Errorf("%w: account=%s posting=%s", ErrCurrencyMismatch, acct.Currency, currSpec.Code)
	}
	if acct.Status == StatusClosed {
		return Account{}, ErrAccountClosed
	}

	delta, err := ComputeBalanceDelta(acct.Category, side, amountMinor)
	if err != nil {
		return Account{}, err
	}

	// FROZEN_COMPLIANCE permits inbound credits/recoveries (delta > 0) while blocking outbound withdrawals (delta < 0).
	if acct.Status == StatusFrozenCompliance && delta < 0 {
		return Account{}, fmt.Errorf("%w: account=%s", ErrAccountComplianceFrozen, acct.Code)
	}

	nextBal, err := currency.CheckedAdd(acct.BalanceMinor, delta)
	if err != nil {
		return Account{}, err
	}
	if !acct.AllowOverdraft && nextBal < 0 {
		return Account{}, fmt.Errorf("%w: account=%s available=%d required=%d", ErrInsufficientLedgerFunds, acct.Code, acct.BalanceMinor, -delta)
	}

	acct.BalanceMinor = nextBal
	return *acct, nil
}

// Get returns a snapshot copy of an account's metadata and current balance.
func (r *Registry) Get(code string) (Account, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	acct, ok := r.accounts[strings.TrimSpace(code)]
	if !ok {
		return Account{}, ErrAccountNotFound
	}
	return *acct, nil
}
