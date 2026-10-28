package scalars_test

import (
	"testing"
	"time"

	"github.com/xdvi/stencil/scalars"
)

func TestParseISO8601_Valid(t *testing.T) {
	got, err := scalars.ParseISO8601([]byte("2026-10-28T17:30:00Z"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Date(2026, 10, 28, 17, 30, 0, 0, time.UTC)
	if !got.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, got)
	}
}

func TestParseISO8601_Invalid(t *testing.T) {
	invalids := []string{
		"2026-02-31T00:00:00Z",       // Invalid day
		"2026-02-29T00:00:00Z",       // Not leap
		"2026-10-28T17:30:00Z_EXTRA", // Trailing junk
		"short",
	}
	for _, s := range invalids {
		if _, err := scalars.ParseISO8601([]byte(s)); err == nil {
			t.Errorf("expected error for invalid ISO8601 %q", s)
		}
	}
}

func TestParseCurrencyCents_Valid(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"1250.50", 125050},
		{"$1,250.50", 125050},
		{"0.99", 99},
		{"-45.10", -4510},
	}
	for _, tt := range tests {
		got, err := scalars.ParseCurrencyCents([]byte(tt.input))
		if err != nil || got != tt.expected {
			t.Fatalf("input %q failed: got %d, err %v", tt.input, got, err)
		}
	}
}

func TestParseCurrencyCents_Invalid(t *testing.T) {
	invalids := []string{"10.999", "---", ".", "12-34", ",100.00"}
	for _, s := range invalids {
		if _, err := scalars.ParseCurrencyCents([]byte(s)); err == nil {
			t.Errorf("expected error for invalid currency %q", s)
		}
	}
}

func BenchmarkParseISO8601_ZeroAlloc(b *testing.B) {
	raw := []byte("2026-10-28T17:30:00Z")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = scalars.ParseISO8601(raw)
	}
}
