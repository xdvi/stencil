package sink_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xdvi/stencil"
	"github.com/xdvi/stencil/drivers/csv"
	"github.com/xdvi/stencil/sink"
)

func TestAppendISO8601UTC(t *testing.T) {
	cases := []struct {
		input time.Time
		want  string
	}{
		{time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC), "2026-10-28T12:00:00Z"},
		{time.Date(2024, 2, 29, 9, 5, 4, 0, time.UTC), "2024-02-29T09:05:04Z"}, // Leap year
		{time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("EST", -5*3600)), "2026-01-01T05:00:00Z"},
	}

	for _, tc := range cases {
		got := string(sink.AppendISO8601UTC(nil, tc.input))
		if got != tc.want {
			t.Errorf("AppendISO8601UTC(%v): expected %q, got %q", tc.input, tc.want, got)
		}
	}
}

func TestAppendISO4217Cents(t *testing.T) {
	cases := []struct {
		cents int64
		want  string
	}{
		{0, "0.00"},
		{5, "0.05"},
		{50, "0.50"},
		{1250, "12.50"},
		{-1250, "-12.50"},
		{-5, "-0.05"},
		{100000000, "1000000.00"},
	}

	for _, tc := range cases {
		got := string(sink.AppendISO4217Cents(nil, tc.cents))
		if got != tc.want {
			t.Errorf("AppendISO4217Cents(%d): expected %q, got %q", tc.cents, tc.want, got)
		}
	}
}

type OrderRecord struct {
	ID        string    `stencil:"col:0,key:id,required"`
	Amount    int64     `stencil:"col:1,key:amount,currency"`
	CreatedAt time.Time `stencil:"col:2,key:created_at,iso8601"`
	Notes     string    `stencil:"col:3,key:notes"`
}

func TestStreamEncoder_CSVRoundTrip(t *testing.T) {
	s, err := stencil.Compile[OrderRecord]()
	if err != nil {
		t.Fatal(err)
	}

	orders := []OrderRecord{
		{
			ID:        "ORD-001",
			Amount:    1250,
			CreatedAt: time.Date(2026, 10, 28, 14, 30, 0, 0, time.UTC),
			Notes:     "Fast shipment, \"fragile\"",
		},
		{
			ID:        "ORD-002",
			Amount:    9999,
			CreatedAt: time.Date(2026, 10, 28, 15, 0, 0, 0, time.UTC),
			Notes:     "Deliver by Monday\nCall on arrival",
		},
	}

	var buf bytes.Buffer
	csvEmitter := sink.NewCSVEmitter(&buf)
	encoder, err := sink.NewStreamEncoder(s, csvEmitter)
	if err != nil {
		t.Fatal(err)
	}

	if err := encoder.EncodeAll(orders); err != nil {
		t.Fatalf("failed to encode: %v", err)
	}

	// Now scan back using csv scanner
	scanner := csv.NewScanner(bytes.NewReader(buf.Bytes()))
	var decoded []OrderRecord
	for item, rowErr := range stencil.ScanCopy[OrderRecord](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatalf("scan error: %+v", rowErr)
		}
		decoded = append(decoded, item)
	}

	if len(decoded) != len(orders) {
		t.Fatalf("expected %d items, got %d", len(orders), len(decoded))
	}

	for i := range orders {
		if decoded[i].ID != orders[i].ID {
			t.Errorf("[%d] ID mismatch: %q != %q", i, decoded[i].ID, orders[i].ID)
		}
		if decoded[i].Amount != orders[i].Amount {
			t.Errorf("[%d] Amount mismatch: %d != %d", i, decoded[i].Amount, orders[i].Amount)
		}
		if !decoded[i].CreatedAt.Equal(orders[i].CreatedAt) {
			t.Errorf("[%d] CreatedAt mismatch: %v != %v", i, decoded[i].CreatedAt, orders[i].CreatedAt)
		}
		if decoded[i].Notes != orders[i].Notes {
			t.Errorf("[%d] Notes mismatch: %q != %q", i, decoded[i].Notes, orders[i].Notes)
		}
	}
}

func BenchmarkFastISO8601UTC(b *testing.B) {
	ts := time.Now().UTC()
	var buf [32]byte

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = sink.AppendISO8601UTC(buf[:0], ts)
	}
}

func BenchmarkStandardTimeFormat(b *testing.B) {
	ts := time.Now().UTC()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ts.Format(time.RFC3339)
	}
}

func BenchmarkFastCurrencyCents(b *testing.B) {
	var buf [32]byte
	cents := int64(125095)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = sink.AppendISO4217Cents(buf[:0], cents)
	}
}

func BenchmarkSprintfCurrency(b *testing.B) {
	cents := int64(125095)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("%.2f", float64(cents)/100.0)
	}
}
