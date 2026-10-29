package stencil_test

import (
	"strings"
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

type goodKeyOnly struct {
	ID string `stencil:"key:id,required"`
}

type badUnknownTag struct {
	A string `stencil:"col:0,bogus"`
}

type badEmptyToken struct {
	A string `stencil:"col:0,,required"`
}

type badTrailingComma struct {
	A string `stencil:"col:0,"`
}

type badEmptyCol struct {
	A string `stencil:"col:"`
}

type badNegativeCol struct {
	A string `stencil:"col:-1"`
}

type badSignedCol struct {
	A string `stencil:"col:+1"`
}

type badDuplicateColTag struct {
	A string `stencil:"col:0,col:1"`
}

type badDuplicateColFields struct {
	A string `stencil:"col:0"`
	B string `stencil:"col:0"`
}

type badEmptyKey struct {
	A string `stencil:"key:"`
}

type badDuplicateKeyTag struct {
	A string `stencil:"key:a,key:b"`
}

type badDuplicateKeyFields struct {
	A string `stencil:"key:a"`
	B string `stencil:"key:a"`
}

type badUnbound struct {
	A string `stencil:"required"`
}

type badUnexported struct {
	a string `stencil:"col:0"`
}

type badCurrencyString struct {
	A string `stencil:"col:0,currency"`
}

type badISOInt struct {
	A int64 `stencil:"col:0,iso8601"`
}

type badUnknownType struct {
	A bool `stencil:"col:0"`
}

func compileNoPanic(t *testing.T, fn func() error) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Compile panicked: %v", r)
		}
	}()
	return fn()
}

func TestCompile_RejectsInvalidSchema(t *testing.T) {
	cases := []struct {
		name    string
		field   string
		wantSub string
		compile func() error
	}{
		{
			name:    "unknown tag token",
			field:   "A",
			wantSub: `unknown tag token "bogus"`,
			compile: func() error { _, err := stencil.Compile[badUnknownTag](); return err },
		},
		{
			name:    "empty token",
			field:   "A",
			wantSub: "empty tag token",
			compile: func() error { _, err := stencil.Compile[badEmptyToken](); return err },
		},
		{
			name:    "trailing comma",
			field:   "A",
			wantSub: "empty tag token",
			compile: func() error { _, err := stencil.Compile[badTrailingComma](); return err },
		},
		{
			name:    "empty col",
			field:   "A",
			wantSub: "empty col index",
			compile: func() error { _, err := stencil.Compile[badEmptyCol](); return err },
		},
		{
			name:    "negative col",
			field:   "A",
			wantSub: `invalid col index "-1"`,
			compile: func() error { _, err := stencil.Compile[badNegativeCol](); return err },
		},
		{
			name:    "signed col",
			field:   "A",
			wantSub: `invalid col index "+1"`,
			compile: func() error { _, err := stencil.Compile[badSignedCol](); return err },
		},
		{
			name:    "duplicate col in tag",
			field:   "A",
			wantSub: "duplicate col:0",
			compile: func() error { _, err := stencil.Compile[badDuplicateColTag](); return err },
		},
		{
			name:    "duplicate col across fields",
			field:   "B",
			wantSub: "duplicate col:0 (already used by A)",
			compile: func() error { _, err := stencil.Compile[badDuplicateColFields](); return err },
		},
		{
			name:    "empty key",
			field:   "A",
			wantSub: "empty key name",
			compile: func() error { _, err := stencil.Compile[badEmptyKey](); return err },
		},
		{
			name:    "duplicate key in tag",
			field:   "A",
			wantSub: `duplicate key "a"`,
			compile: func() error { _, err := stencil.Compile[badDuplicateKeyTag](); return err },
		},
		{
			name:    "duplicate key across fields",
			field:   "B",
			wantSub: `duplicate key "a" (already used by A)`,
			compile: func() error { _, err := stencil.Compile[badDuplicateKeyFields](); return err },
		},
		{
			name:    "tag without col or key",
			field:   "A",
			wantSub: "tag must declare col or key",
			compile: func() error { _, err := stencil.Compile[badUnbound](); return err },
		},
		{
			name:    "unexported field",
			field:   "a",
			wantSub: "unexported",
			compile: func() error { _, err := stencil.Compile[badUnexported](); return err },
		},
		{
			name:    "currency on string",
			field:   "A",
			wantSub: "currency requires an int64 field",
			compile: func() error { _, err := stencil.Compile[badCurrencyString](); return err },
		},
		{
			name:    "iso8601 on int64",
			field:   "A",
			wantSub: "iso8601 requires a time.Time field",
			compile: func() error { _, err := stencil.Compile[badISOInt](); return err },
		},
		{
			name:    "unsupported type",
			field:   "A",
			wantSub: "unsupported type bool",
			compile: func() error { _, err := stencil.Compile[badUnknownType](); return err },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := compileNoPanic(t, tc.compile)
			if err == nil {
				t.Fatalf("expected Compile to fail, got nil")
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.wantSub) {
				t.Fatalf("error %q does not contain %q", msg, tc.wantSub)
			}
			wantPrefix := "stencil: field " + tc.field + ": "
			if !strings.HasPrefix(msg, wantPrefix) {
				t.Fatalf("error %q does not start with %q", msg, wantPrefix)
			}
		})
	}
}

func TestCompile_AcceptsKeyOnlyField(t *testing.T) {
	s, err := stencil.Compile[goodKeyOnly]()
	if err != nil {
		t.Fatalf("key-only schema rejected: %v", err)
	}

	var doc goodKeyOnly
	s.ResetAndBind(&doc)
	var rowErr stencil.RowError
	s.SetField(&doc, 1, -1, []byte("id"), []byte("DOC-1"), &rowErr)
	if doc.ID != "DOC-1" {
		t.Fatalf("key bind failed: %+v", doc)
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
