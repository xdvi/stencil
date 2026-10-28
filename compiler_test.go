package stencil_test

import (
	"testing"
	"time"

	"github.com/xdvi/stencil"
)

type UnifiedDoc struct {
	ID        string    `stencil:"col:0,key:id,required"`
	Amount    int64     `stencil:"col:1,key:amount,currency"`
	CreatedAt time.Time `stencil:"col:2,key:created_at,iso8601"`
	Note      string    `stencil:"col:3,key:note"`
}

func TestSchema_BindPositional_And_Named(t *testing.T) {
	s, err := stencil.Compile[UnifiedDoc]()
	if err != nil {
		t.Fatal(err)
	}

	// 1. Positional binding (CSV/XLSX mode)
	var doc UnifiedDoc
	s.ResetAndBind(&doc)
	var rowErr stencil.RowError

	s.SetField(&doc, 1, 0, nil, []byte("DOC-1"), &rowErr)
	s.SetField(&doc, 1, 1, nil, []byte("$10.50"), &rowErr)
	s.SetField(&doc, 1, 2, nil, []byte("2026-10-28T12:00:00Z"), &rowErr)
	s.SetField(&doc, 1, 3, nil, []byte("Paid"), &rowErr)

	if doc.ID != "DOC-1" || doc.Amount != 1050 || doc.Note != "Paid" {
		t.Fatalf("positional bind failed: %+v", doc)
	}

	// 2. Data bleed prevention across row iterations
	s.ResetAndBind(&doc)
	s.SetField(&doc, 2, 0, nil, []byte("DOC-2"), &rowErr)
	if doc.Note != "" {
		t.Fatalf("data bleed detected: note was not reset: %q", doc.Note)
	}

	// 3. Named key binding (JSON/XML mode)
	s.ResetAndBind(&doc)
	s.SetField(&doc, 3, -1, []byte("id"), []byte("DOC-JSON"), &rowErr)
	s.SetField(&doc, 3, -1, []byte("amount"), []byte("99.00"), &rowErr)
	if doc.ID != "DOC-JSON" || doc.Amount != 9900 {
		t.Fatalf("named bind failed: %+v", doc)
	}
}

func BenchmarkSetField_ZeroAlloc(b *testing.B) {
	s, err := stencil.Compile[UnifiedDoc]()
	if err != nil {
		b.Fatal(err)
	}
	var doc UnifiedDoc
	var rowErr stencil.RowError
	val := []byte("DOC-1")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.SetField(&doc, 1, 0, nil, val, &rowErr)
	}
}
