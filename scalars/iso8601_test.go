package scalars_test

import (
	"errors"
	"testing"
	"time"

	"github.com/xdvi/stencil/scalars"
)

func TestParseISO8601_ValidTable(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Time
	}{
		{"2026-10-28T00:00:00Z", time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)},
		{"2026-10-28T23:59:59Z", time.Date(2026, 10, 28, 23, 59, 59, 0, time.UTC)},
		{"2024-02-29T12:34:56Z", time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC)},
		{"0001-01-01T00:00:00Z", time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)},
	}

	for _, tt := range tests {
		got, err := scalars.ParseISO8601([]byte(tt.input))
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tt.input, err)
			continue
		}
		if !got.Equal(tt.expected) {
			t.Errorf("%q: expected %v, got %v", tt.input, tt.expected, got)
		}
	}
}

func TestParseISO8601_InvalidTable(t *testing.T) {
	tests := []string{
		"2026-10-28T-1:00:00Z", // negative hour
		"2026-10-28T00:-1:00Z", // negative minute
		"2026-10-28T00:00:0-Z", // negative second
		"2026-10-28T0a:00:00Z", // non-digit hour
		"2026-10-28T00:0a:00Z", // non-digit minute
		"2026-10-28T24:00:00Z", // hour out of range
		"2026-02-29T00:00:00Z", // non-leap February 29
		"0000-01-01T00:00:00Z", // year < 1
		"2026-00-10T00:00:00Z", // month 0
		"2026-13-10T00:00:00Z", // month 13
		"2026-04-31T00:00:00Z", // April has 30 days
	}

	for _, s := range tests {
		got, err := scalars.ParseISO8601([]byte(s))
		if !errors.Is(err, scalars.ErrInvalidISO8601) {
			t.Errorf("%q: expected ErrInvalidISO8601, got err=%v value=%v", s, err, got)
		}
	}
}
