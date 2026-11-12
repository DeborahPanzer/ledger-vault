package currency

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
)

var (
	// ErrUnsupportedCurrency indicates an unknown or malformed ISO-4217 alphabetic code.
	ErrUnsupportedCurrency = errors.New("currency: unsupported ISO-4217 alphabetic code")
	// ErrMonetaryOverflow indicates an int64 addition, subtraction, or basis-point multiplication overflowed.
	ErrMonetaryOverflow = errors.New("currency: int64 monetary arithmetic overflow")
	// ErrNegativeBasisPoints indicates negative amount or basis-point inputs where non-negative values are required.
	ErrNegativeBasisPoints = errors.New("currency: amount and basis points must be non-negative")
)

// Spec defines the ISO-4217 metadata and minor-unit exponent for a currency.
type Spec struct {
	Code        string
	NumericCode string
	Exponent    uint8
}

var registry = map[string]Spec{
	// Zero-decimal currencies (Exponent = 0)
	"BIF": {Code: "BIF", NumericCode: "108", Exponent: 0},
	"CLP": {Code: "CLP", NumericCode: "152", Exponent: 0},
	"JPY": {Code: "JPY", NumericCode: "392", Exponent: 0},
	"KRW": {Code: "KRW", NumericCode: "410", Exponent: 0},
	"VND": {Code: "VND", NumericCode: "704", Exponent: 0},

	// Standard two-decimal currencies (Exponent = 2)
	"AUD": {Code: "AUD", NumericCode: "036", Exponent: 2},
	"BRL": {Code: "BRL", NumericCode: "986", Exponent: 2},
	"CAD": {Code: "CAD", NumericCode: "124", Exponent: 2},
	"CHF": {Code: "CHF", NumericCode: "756", Exponent: 2},
	"EUR": {Code: "EUR", NumericCode: "978", Exponent: 2},
	"GBP": {Code: "GBP", NumericCode: "826", Exponent: 2},
	"HKD": {Code: "HKD", NumericCode: "344", Exponent: 2},
	"INR": {Code: "INR", NumericCode: "356", Exponent: 2},
	"MXN": {Code: "MXN", NumericCode: "484", Exponent: 2},
	"NOK": {Code: "NOK", NumericCode: "578", Exponent: 2},
	"NZD": {Code: "NZD", NumericCode: "554", Exponent: 2},
	"PLN": {Code: "PLN", NumericCode: "985", Exponent: 2},
	"SEK": {Code: "SEK", NumericCode: "752", Exponent: 2},
	"SGD": {Code: "SGD", NumericCode: "702", Exponent: 2},
	"USD": {Code: "USD", NumericCode: "840", Exponent: 2},

	// Three-decimal currencies (Exponent = 3)
	"BHD": {Code: "BHD", NumericCode: "048", Exponent: 3},
	"JOD": {Code: "JOD", NumericCode: "400", Exponent: 3},
	"KWD": {Code: "KWD", NumericCode: "414", Exponent: 3},
	"OMR": {Code: "OMR", NumericCode: "512", Exponent: 3},
	"TND": {Code: "TND", NumericCode: "788", Exponent: 3},

	// Four-decimal accounting units (Exponent = 4)
	"CLF": {Code: "CLF", NumericCode: "990", Exponent: 4},
}

// Lookup normalizes and validates an ISO-4217 currency code.
func Lookup(rawCode string) (Spec, error) {
	normalized := strings.ToUpper(strings.TrimSpace(rawCode))
	spec, ok := registry[normalized]
	if !ok {
		return Spec{}, fmt.Errorf("%w: %q", ErrUnsupportedCurrency, rawCode)
	}
	return spec, nil
}

// CheckedAdd returns a + b or ErrMonetaryOverflow if the result exceeds int64 bounds.
func CheckedAdd(a, b int64) (int64, error) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, ErrMonetaryOverflow
	}
	return a + b, nil
}

// CheckedSub returns a - b or ErrMonetaryOverflow if the result exceeds int64 bounds.
func CheckedSub(a, b int64) (int64, error) {
	if (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b) {
		return 0, ErrMonetaryOverflow
	}
	return a - b, nil
}

// CheckedMulBps computes (amountMinor * bps) / 10,000 using exact big.Int intermediates
// and deterministic IEEE-754 Round-Half-to-Even (Banker's Rounding).
func CheckedMulBps(amountMinor int64, bps int64) (int64, error) {
	if amountMinor < 0 || bps < 0 {
		return 0, ErrNegativeBasisPoints
	}
	if amountMinor == 0 || bps == 0 {
		return 0, nil
	}

	prod := new(big.Int).Mul(big.NewInt(amountMinor), big.NewInt(bps))
	denom := big.NewInt(10_000)
	quo, rem := new(big.Int), new(big.Int)
	quo.QuoRem(prod, denom, rem)

	half := big.NewInt(5_000)
	cmp := rem.Cmp(half)
	if cmp > 0 || (cmp == 0 && quo.Bit(0) == 1) {
		quo.Add(quo, big.NewInt(1))
	}

	if !quo.IsInt64() {
		return 0, ErrMonetaryOverflow
	}
	return quo.Int64(), nil
}

// FormatDecimal renders an int64 minor-unit balance as a canonical fixed-point decimal string.
func FormatDecimal(amountMinor int64, rawCode string) (string, error) {
	spec, err := Lookup(rawCode)
	if err != nil {
		return "", err
	}

	sign := ""
	mag := new(big.Int).SetInt64(amountMinor)
	if mag.Sign() < 0 {
		sign = "-"
		mag.Neg(mag)
	}

	digits := mag.String()
	exp := int(spec.Exponent)
	if exp == 0 {
		return sign + digits, nil
	}

	if len(digits) <= exp {
		digits = strings.Repeat("0", exp-len(digits)+1) + digits
	}
	split := len(digits) - exp
	return sign + digits[:split] + "." + digits[split:], nil
}
