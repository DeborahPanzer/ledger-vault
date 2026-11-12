package currency

import (
	"errors"
	"math"
	"testing"
)

func TestISO4217RegistryAndFormatting(t *testing.T) {
	cases := []struct {
		code   string
		amount int64
		want   string
	}{
		{"usd", 12345, "123.45"},
		{"USD", -5, "-0.05"},
		{"JPY", 5800, "5800"},
		{"VND", 250000, "250000"},
		{"KWD", 1500, "1.500"},
		{"CLF", 90001, "9.0001"},
		{"EUR", math.MinInt64, "-92233720368547758.08"},
	}

	for _, tc := range cases {
		got, err := FormatDecimal(tc.amount, tc.code)
		if err != nil {
			t.Fatalf("FormatDecimal(%d, %q) unexpected error: %v", tc.amount, tc.code, err)
		}
		if got != tc.want {
			t.Fatalf("FormatDecimal(%d, %q) = %q, want %q", tc.amount, tc.code, got, tc.want)
		}
	}

	if _, err := Lookup("XYZ"); !errors.Is(err, ErrUnsupportedCurrency) {
		t.Fatalf("expected ErrUnsupportedCurrency for XYZ, got %v", err)
	}
}

func TestCheckedArithmeticAndBankersRounding(t *testing.T) {
	if _, err := CheckedAdd(math.MaxInt64, 1); !errors.Is(err, ErrMonetaryOverflow) {
		t.Fatalf("expected overflow on MaxInt64 + 1, got %v", err)
	}
	if _, err := CheckedSub(math.MinInt64, 1); !errors.Is(err, ErrMonetaryOverflow) {
		t.Fatalf("expected underflow on MinInt64 - 1, got %v", err)
	}

	// Exact tie-breaking tests at 5000 bps (50.00%):
	// 1 * 0.5 = 0.5 -> rounds to even 0
	// 3 * 0.5 = 1.5 -> rounds to even 2
	// 5 * 0.5 = 2.5 -> rounds to even 2
	// 7 * 0.5 = 3.5 -> rounds to even 4
	ties := []struct {
		amount int64
		want   int64
	}{
		{1, 0},
		{3, 2},
		{5, 2},
		{7, 4},
	}
	for _, tc := range ties {
		got, err := CheckedMulBps(tc.amount, 5_000)
		if err != nil || got != tc.want {
			t.Fatalf("CheckedMulBps(%d, 5000) = (%d, %v), want (%d, nil)", tc.amount, got, err, tc.want)
		}
	}
}
