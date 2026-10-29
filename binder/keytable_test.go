package binder_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/xdvi/stencil/binder"
)

func TestKeyTable_BasicAndCollisions(t *testing.T) {
	entries := []binder.KeyEntry{
		{Key: []byte("id"), FieldIdx: 0},
		{Key: []byte("username"), FieldIdx: 1},
		{Key: []byte("customer_id"), FieldIdx: 2},
		{Key: []byte("customer_name"), FieldIdx: 3},
		{Key: []byte("customer_email"), FieldIdx: 4},
		{Key: []byte("amount"), FieldIdx: 5},
		{Key: []byte("created_at"), FieldIdx: 6},
		{Key: []byte("x"), FieldIdx: 7},
	}

	table, err := binder.NewKeyTable(entries)
	if err != nil {
		t.Fatalf("failed to compile KeyTable: %v", err)
	}

	if table.Len() != len(entries) {
		t.Fatalf("expected %d entries, got %d", len(entries), table.Len())
	}

	// Verify all existing keys match exactly
	for _, e := range entries {
		idx := table.Lookup(e.Key)
		if idx != e.FieldIdx {
			t.Errorf("Lookup(%s): expected %d, got %d", e.Key, e.FieldIdx, idx)
		}

		strIdx := table.LookupString(string(e.Key))
		if strIdx != e.FieldIdx {
			t.Errorf("LookupString(%s): expected %d, got %d", e.Key, e.FieldIdx, strIdx)
		}
	}

	// Non-existent keys
	misses := [][]byte{
		[]byte("unknown"),
		[]byte("customer_"),
		[]byte("customer_ids"),
		[]byte("i"),
		[]byte(""),
	}
	for _, m := range misses {
		if idx := table.Lookup(m); idx != -1 {
			t.Errorf("Lookup(%s): expected -1, got %d", m, idx)
		}
	}
}

func TestKeyTable_DuplicatesAndEmpty(t *testing.T) {
	_, err := binder.NewKeyTable([]binder.KeyEntry{
		{Key: []byte("dup"), FieldIdx: 0},
		{Key: []byte("dup"), FieldIdx: 1},
	})
	if !errors.Is(err, binder.ErrDuplicateKey) {
		t.Errorf("expected ErrDuplicateKey, got %v", err)
	}

	_, err = binder.NewKeyTable([]binder.KeyEntry{
		{Key: []byte(""), FieldIdx: 0},
	})
	if err == nil {
		t.Errorf("expected error on empty key")
	}

	emptyTable, err := binder.NewKeyTable(nil)
	if err != nil {
		t.Fatalf("unexpected error on nil entries: %v", err)
	}
	if emptyTable.Lookup([]byte("any")) != -1 {
		t.Errorf("expected -1 on empty table")
	}
}

func TestKeyTable_ZeroAllocLookup(t *testing.T) {
	table, err := binder.NewKeyTable([]binder.KeyEntry{
		{Key: []byte("invoice_number"), FieldIdx: 0},
		{Key: []byte("payment_status"), FieldIdx: 1},
		{Key: []byte("line_item_sku"), FieldIdx: 2},
	})
	if err != nil {
		t.Fatal(err)
	}

	key := []byte("payment_status")
	allocs := testing.AllocsPerRun(100, func() {
		idx := table.Lookup(key)
		if idx != 1 {
			t.Fatalf("expected 1, got %d", idx)
		}
	})

	if allocs > 0 {
		t.Fatalf("expected 0 allocs on Lookup, got %f", allocs)
	}
}

func BenchmarkKeyTable_Lookup(b *testing.B) {
	keys := []string{
		"id", "user_id", "email_address", "created_at", "amount_cents",
		"currency_code", "status", "tax_identifier", "metadata_payload",
	}
	var entries []binder.KeyEntry
	for i, k := range keys {
		entries = append(entries, binder.KeyEntry{Key: []byte(k), FieldIdx: i})
	}

	table, err := binder.NewKeyTable(entries)
	if err != nil {
		b.Fatal(err)
	}

	target := []byte("amount_cents")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = table.Lookup(target)
	}
}

func BenchmarkLinear_Lookup(b *testing.B) {
	keys := [][]byte{
		[]byte("id"), []byte("user_id"), []byte("email_address"), []byte("created_at"),
		[]byte("amount_cents"), []byte("currency_code"), []byte("status"),
		[]byte("tax_identifier"), []byte("metadata_payload"),
	}
	target := []byte("amount_cents")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for idx, k := range keys {
			if bytes.Equal(k, target) {
				_ = idx
				break
			}
		}
	}
}

func BenchmarkStandardMap_Lookup(b *testing.B) {
	keys := []string{
		"id", "user_id", "email_address", "created_at", "amount_cents",
		"currency_code", "status", "tax_identifier", "metadata_payload",
	}
	m := make(map[string]int, len(keys))
	for i, k := range keys {
		m[k] = i
	}
	target := "amount_cents"

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m[target]
	}
}
