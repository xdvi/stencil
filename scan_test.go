package stencil_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xdvi/stencil"
	"github.com/xdvi/stencil/drivers/csv"
)

type requiredTypedRow struct {
	Amount int64     `stencil:"col:0,currency,required"`
	At     time.Time `stencil:"col:1,iso8601,required"`
}

func TestValidateRequired_TypedFields(t *testing.T) {
	s, err := stencil.Compile[requiredTypedRow]()
	if err != nil {
		t.Fatal(err)
	}

	var zero requiredTypedRow
	errs := s.ValidateRequired(&zero, 1, nil)
	if len(errs) != 2 {
		t.Fatalf("expected 2 required errors for zero values, got %d: %+v", len(errs), errs)
	}
	wantCols := []string{"col:0", "col:1"}
	for i, e := range errs {
		if e.Code != "required_field" {
			t.Errorf("error %d: expected code required_field, got %q", i, e.Code)
		}
		if e.Message != "Field is required" {
			t.Errorf("error %d: expected message %q, got %q", i, "Field is required", e.Message)
		}
		if e.Col != wantCols[i] {
			t.Errorf("error %d: expected col %q, got %q", i, wantCols[i], e.Col)
		}
	}

	valid := requiredTypedRow{
		Amount: 42,
		At:     time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC),
	}
	errs = s.ValidateRequired(&valid, 2, errs)
	if len(errs) != 0 {
		t.Fatalf("expected no errors for valid values, got %+v", errs)
	}
}

func TestScan_RequiredCurrencyAndTime(t *testing.T) {
	s, err := stencil.Compile[requiredTypedRow]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := csv.NewScanner(strings.NewReader("$42.00,2026-10-28T12:00:00Z\n"))
	count := 0
	for row, rowErr := range stencil.Scan[requiredTypedRow](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatalf("unexpected row error: %+v", rowErr)
		}
		count++
		if row.Amount != 4200 {
			t.Errorf("expected amount 4200, got %d", row.Amount)
		}
		want := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
		if !row.At.Equal(want) {
			t.Errorf("expected time %s, got %s", want, row.At)
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 row, got %d", count)
	}
}

type requiredLabelRow struct {
	Name string `stencil:"col:0,required"`
}

func TestScan_RequiredErrorLabel(t *testing.T) {
	s, err := stencil.Compile[requiredLabelRow]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := csv.NewScanner(strings.NewReader(",ignored\n"))
	var got []stencil.RowError
	for _, rowErr := range stencil.Scan[requiredLabelRow](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			got = append(got, rowErr)
		}
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 required error, got %d: %+v", len(got), got)
	}
	if got[0].Code != "required_field" || got[0].Message != "Field is required" {
		t.Fatalf("unexpected error: %+v", got[0])
	}
	if got[0].Col != "col:0" {
		t.Fatalf("expected col label %q, got %q", "col:0", got[0].Col)
	}
}

type copyRow struct {
	Name   string `stencil:"col:0,required"`
	Amount int64  `stencil:"col:1,currency,required"`
}

func TestScanCopy_RetainsRows(t *testing.T) {
	s, err := stencil.Compile[copyRow]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := csv.NewScanner(strings.NewReader("Alice,$1.00\nBob,$2.00\n"))
	var rows []copyRow
	for row, rowErr := range stencil.ScanCopy[copyRow](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatalf("unexpected row error: %+v", rowErr)
		}
		rows = append(rows, row)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Name != "Alice" || rows[0].Amount != 100 {
		t.Errorf("row 0 corrupted: %+v", rows[0])
	}
	if rows[1].Name != "Bob" || rows[1].Amount != 200 {
		t.Errorf("row 1 corrupted: %+v", rows[1])
	}
}

type intRow struct {
	Amount int64 `stencil:"col:0,currency"`
}

func TestScan_YieldsIndependentValues(t *testing.T) {
	s, err := stencil.Compile[intRow]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := csv.NewScanner(strings.NewReader("$1.00\n$2.00\n$3.00\n"))
	var rows []intRow
	for row, rowErr := range stencil.Scan[intRow](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatalf("unexpected row error: %+v", rowErr)
		}
		rows = append(rows, row)
	}

	want := []int64{100, 200, 300}
	if len(rows) != len(want) {
		t.Fatalf("expected %d rows, got %d", len(want), len(rows))
	}
	for i := range want {
		if rows[i].Amount != want[i] {
			t.Errorf("row %d: expected amount %d, got %d", i, want[i], rows[i].Amount)
		}
	}
}
