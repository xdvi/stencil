package stencil_test

import (
	"context"
	"errors"
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

type contextRow struct {
	Name string `stencil:"col:0"`
}

type trackingScanner struct {
	rows       [][]string
	idx        int
	nextCalls  int
	closeCalls int
	onFields   func()
	scanErr    error
	streamErr  error
}

func (s *trackingScanner) Next() bool {
	s.nextCalls++
	if s.idx >= len(s.rows) {
		return false
	}
	s.idx++
	return true
}

func (s *trackingScanner) ScanFields(yield stencil.FieldVisitor) error {
	if s.onFields != nil {
		s.onFields()
	}
	if s.scanErr != nil {
		return s.scanErr
	}
	for col, val := range s.rows[s.idx-1] {
		if !yield(col, nil, []byte(val)) {
			break
		}
	}
	return nil
}

func (s *trackingScanner) Err() error { return s.streamErr }

func (s *trackingScanner) Close() error {
	s.closeCalls++
	return nil
}

func TestScan_PreCanceledContext(t *testing.T) {
	s, err := stencil.Compile[contextRow]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := &trackingScanner{rows: [][]string{{"Alice"}, {"Bob"}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var rows, events int
	var got []stencil.RowError
	for _, rowErr := range stencil.Scan[contextRow](ctx, scanner, s) {
		if rowErr.Code == "" {
			rows++
			continue
		}
		events++
		got = append(got, rowErr)
	}

	if rows != 0 {
		t.Fatalf("expected 0 valid rows, got %d", rows)
	}
	if events != 1 {
		t.Fatalf("expected exactly 1 canceled event, got %d: %+v", events, got)
	}
	if got[0].Code != "canceled" {
		t.Errorf("expected code canceled, got %q", got[0].Code)
	}
	if got[0].Row != 0 {
		t.Errorf("expected row 0, got %d", got[0].Row)
	}
	if got[0].Message != context.Canceled.Error() {
		t.Errorf("expected message %q, got %q", context.Canceled.Error(), got[0].Message)
	}
	if scanner.nextCalls != 0 {
		t.Errorf("expected 0 Next calls, got %d", scanner.nextCalls)
	}
	if scanner.closeCalls != 1 {
		t.Errorf("expected 1 Close call, got %d", scanner.closeCalls)
	}
}

func TestScan_CancelAfterFirstRow(t *testing.T) {
	s, err := stencil.Compile[contextRow]()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	scanner := &trackingScanner{rows: [][]string{{"Alice"}, {"Bob"}}}
	scanner.onFields = func() {
		if scanner.idx == 1 {
			cancel()
		}
	}

	var rows, events int
	var got []stencil.RowError
	for row, rowErr := range stencil.Scan[contextRow](ctx, scanner, s) {
		if rowErr.Code == "" {
			rows++
			if row.Name != "Alice" {
				t.Errorf("expected first row Alice, got %+v", row)
			}
			continue
		}
		events++
		got = append(got, rowErr)
	}

	if rows != 1 {
		t.Fatalf("expected 1 valid row, got %d", rows)
	}
	if events != 1 {
		t.Fatalf("expected exactly 1 canceled event, got %d: %+v", events, got)
	}
	if got[0].Code != "canceled" {
		t.Errorf("expected code canceled, got %q", got[0].Code)
	}
	if got[0].Row != 1 {
		t.Errorf("expected row 1, got %d", got[0].Row)
	}
	if scanner.nextCalls != 1 {
		t.Errorf("expected 1 Next call, got %d", scanner.nextCalls)
	}
	if scanner.closeCalls != 1 {
		t.Errorf("expected 1 Close call, got %d", scanner.closeCalls)
	}
}

func TestScan_NormalExhaustionCloses(t *testing.T) {
	s, err := stencil.Compile[contextRow]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := &trackingScanner{rows: [][]string{{"Alice"}, {"Bob"}}}
	var rows int
	for row, rowErr := range stencil.Scan[contextRow](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatalf("unexpected row error: %+v", rowErr)
		}
		rows++
		if rows == 2 && row.Name != "Bob" {
			t.Errorf("expected second row Bob, got %+v", row)
		}
	}

	if rows != 2 {
		t.Fatalf("expected 2 valid rows, got %d", rows)
	}
	if scanner.closeCalls != 1 {
		t.Errorf("expected 1 Close call, got %d", scanner.closeCalls)
	}
}

func TestScan_EarlyBreakCloses(t *testing.T) {
	s, err := stencil.Compile[contextRow]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := &trackingScanner{rows: [][]string{{"Alice"}, {"Bob"}}}
	var rows int
	for _, rowErr := range stencil.Scan[contextRow](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatalf("unexpected row error: %+v", rowErr)
		}
		rows++
		break
	}

	if rows != 1 {
		t.Fatalf("expected 1 valid row, got %d", rows)
	}
	if scanner.closeCalls != 1 {
		t.Errorf("expected 1 Close call, got %d", scanner.closeCalls)
	}
}

func TestScan_ScannerWithoutClose(t *testing.T) {
	s, err := stencil.Compile[contextRow]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := csv.NewScanner(strings.NewReader("Alice\nBob\n"))
	var rows int
	for _, rowErr := range stencil.Scan[contextRow](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatalf("unexpected row error: %+v", rowErr)
		}
		rows++
	}

	if rows != 2 {
		t.Fatalf("expected 2 valid rows, got %d", rows)
	}
}

func TestScan_FieldErrorEarlyStopCloses(t *testing.T) {
	s, err := stencil.Compile[intRow]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := &trackingScanner{rows: [][]string{{"not-a-currency"}, {"$2.00"}}}
	var events int
	for _, rowErr := range stencil.Scan[intRow](context.Background(), scanner, s) {
		if rowErr.Code == "" {
			t.Fatalf("expected field error, got valid row")
		}
		events++
		break
	}

	if events != 1 {
		t.Fatalf("expected 1 field error, got %d", events)
	}
	if scanner.closeCalls != 1 {
		t.Errorf("expected 1 Close call, got %d", scanner.closeCalls)
	}
}

func TestScan_StreamErrorCloses(t *testing.T) {
	s, err := stencil.Compile[contextRow]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := &trackingScanner{
		rows:      [][]string{{"Alice"}},
		streamErr: errors.New("stream broken"),
	}
	var events int
	var got stencil.RowError
	for _, rowErr := range stencil.Scan[contextRow](context.Background(), scanner, s) {
		if rowErr.Code == "" {
			continue
		}
		events++
		got = rowErr
	}

	if events != 1 {
		t.Fatalf("expected 1 stream error, got %d", events)
	}
	if got.Code != "stream_error" {
		t.Errorf("expected code stream_error, got %q", got.Code)
	}
	if scanner.closeCalls != 1 {
		t.Errorf("expected 1 Close call, got %d", scanner.closeCalls)
	}
}

func TestScan_UnmarkedScanFieldsErrorIsStreamError(t *testing.T) {
	s, err := stencil.Compile[contextRow]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := &trackingScanner{
		rows:    [][]string{{"Alice"}},
		scanErr: errors.New("disk read failure"),
	}

	var events []stencil.RowError
	for _, rowErr := range stencil.Scan[contextRow](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			events = append(events, rowErr)
		}
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	if events[0].Code != "stream_error" {
		t.Errorf("expected stream_error for a read-side failure, got %+v", events[0])
	}
	if pd := events[0].ProblemDetails(); pd.Status != 500 {
		t.Errorf("expected status 500, got %d", pd.Status)
	}
}

func TestScan_MarkedStreamErrorIsParseError(t *testing.T) {
	s, err := stencil.Compile[contextRow]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := &trackingScanner{
		rows:      [][]string{{"Alice"}},
		streamErr: stencil.MarkMalformedInput(errors.New("csv: unterminated quoted field")),
	}

	var events []stencil.RowError
	for _, rowErr := range stencil.Scan[contextRow](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			events = append(events, rowErr)
		}
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	if events[0].Code != "parse_error" {
		t.Errorf("expected parse_error, got %+v", events[0])
	}
	if !strings.Contains(events[0].Message, "unterminated quoted field") {
		t.Errorf("expected message to keep the cause, got %q", events[0].Message)
	}
	if pd := events[0].ProblemDetails(); pd.Status != 400 {
		t.Errorf("expected status 400, got %d", pd.Status)
	}
}

func TestScanCopy_ClosesScanner(t *testing.T) {
	s, err := stencil.Compile[contextRow]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := &trackingScanner{rows: [][]string{{"Alice"}, {"Bob"}}}
	var rows int
	for _, rowErr := range stencil.ScanCopy[contextRow](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatalf("unexpected row error: %+v", rowErr)
		}
		rows++
	}

	if rows != 2 {
		t.Fatalf("expected 2 valid rows, got %d", rows)
	}
	if scanner.closeCalls != 1 {
		t.Errorf("expected 1 Close call, got %d", scanner.closeCalls)
	}
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
