package schema_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xdvi/stencil"
	"github.com/xdvi/stencil/drivers/csv"
	"github.com/xdvi/stencil/schema"
)

type FluentRecord struct {
	ID        string
	Amount    int64
	Timestamp time.Time
	Quantity  int
}

type TagRecord struct {
	ID        string    `stencil:"col:0,key:id,required"`
	Amount    int64     `stencil:"col:1,key:amount,currency"`
	Timestamp time.Time `stencil:"col:2,key:ts,iso8601"`
}

func TestSchemaBuilder_BasicAndScan(t *testing.T) {
	b := schema.NewBuilder[FluentRecord]()
	b.FieldString("id", func(r *FluentRecord) *string { return &r.ID }, schema.WithCol(0), schema.WithKey("id"), schema.WithRequired())
	b.FieldCurrency("amount", func(r *FluentRecord) *int64 { return &r.Amount }, schema.WithCol(1), schema.WithKey("amount"))
	b.FieldISO8601("timestamp", func(r *FluentRecord) *time.Time { return &r.Timestamp }, schema.WithCol(2), schema.WithKey("ts"))

	schema.FieldCustom(
		b,
		"quantity",
		func(r *FluentRecord) *int { return &r.Quantity },
		func(b []byte) (int, error) {
			return strconv.Atoi(string(b))
		},
		func(v int, dst []byte) []byte {
			return strconv.AppendInt(dst, int64(v), 10)
		},
		func(v int) bool { return v == 0 },
		schema.WithCol(3),
		schema.WithKey("qty"),
	)

	s, err := b.Build()
	if err != nil {
		t.Fatalf("failed to build schema: %v", err)
	}

	csvData := "REC-101,\"$45.99\",2026-10-28T10:00:00Z,42\nREC-102,\"$12.50\",2026-10-28T11:00:00Z,5\n"
	scanner := csv.NewScanner(strings.NewReader(csvData))

	var records []FluentRecord
	for item, rowErr := range stencil.ScanCopy[FluentRecord](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatalf("unexpected scan error: %+v", rowErr)
		}
		records = append(records, item)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].ID != "REC-101" || records[0].Amount != 4599 || records[0].Quantity != 42 {
		t.Errorf("record 0 mismatch: %+v", records[0])
	}
	if records[1].ID != "REC-102" || records[1].Amount != 1250 || records[1].Quantity != 5 {
		t.Errorf("record 1 mismatch: %+v", records[1])
	}
}

func TestSchemaBuilder_Errors(t *testing.T) {
	// Empty builder
	b1 := schema.NewBuilder[FluentRecord]()
	if _, err := b1.Build(); err == nil {
		t.Error("expected error for empty builder")
	}

	// Missing col and key
	b2 := schema.NewBuilder[FluentRecord]()
	b2.FieldString("id", func(r *FluentRecord) *string { return &r.ID })
	if _, err := b2.Build(); err == nil {
		t.Error("expected error when neither col nor key is specified")
	}

	// Duplicate column
	b3 := schema.NewBuilder[FluentRecord]()
	b3.FieldString("id1", func(r *FluentRecord) *string { return &r.ID }, schema.WithCol(0))
	b3.FieldString("id2", func(r *FluentRecord) *string { return &r.ID }, schema.WithCol(0))
	if _, err := b3.Build(); err == nil {
		t.Error("expected error for duplicate column index")
	}
}

func BenchmarkCompile_ReflectionVsFluent(b *testing.B) {
	b.Run("ReflectionTagCompile", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = stencil.Compile[TagRecord]()
		}
	})

	b.Run("FluentPointerArithmeticBuild", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			builder := schema.NewBuilder[FluentRecord]()
			builder.FieldString("id", func(r *FluentRecord) *string { return &r.ID }, schema.WithCol(0), schema.WithKey("id"), schema.WithRequired())
			builder.FieldCurrency("amount", func(r *FluentRecord) *int64 { return &r.Amount }, schema.WithCol(1), schema.WithKey("amount"))
			builder.FieldISO8601("ts", func(r *FluentRecord) *time.Time { return &r.Timestamp }, schema.WithCol(2), schema.WithKey("ts"))
			_, _ = builder.Build()
		}
	})
}
