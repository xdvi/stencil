package csv_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xdvi/stencil"
	"github.com/xdvi/stencil/drivers/csv"
)

type Account struct {
	Name    string `stencil:"col:0,required"`
	Balance int64  `stencil:"col:1,currency"`
}

type NoteRow struct {
	Name string `stencil:"col:0,required"`
	Note string `stencil:"col:1"`
}

func TestCSV_Driver_Scan(t *testing.T) {
	data := "Alice,\"$1,250.00\"\nBob,$50.25\n"
	s, err := stencil.Compile[Account]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := csv.NewScanner(strings.NewReader(data))
	count := 0
	for item, rowErr := range stencil.Scan[Account](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatal(rowErr)
		}
		count++
		if count == 1 && (item.Name != "Alice" || item.Balance != 125000) {
			t.Errorf("unexpected item: %+v", item)
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 items, got %d", count)
	}
}

func TestCSVScanner_StripsUTF8BOM(t *testing.T) {
	rows := 0
	err := scanEach(t, "\xef\xbb\xbfAlice,$1.00\nBob,$2.00\n", nil, func(a Account) {
		if rows == 0 && a.Name != "Alice" {
			t.Errorf("expected first name Alice, got %q", a.Name)
		}
		rows++
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows != 2 {
		t.Fatalf("expected 2 rows, got %d", rows)
	}
}

func TestCSVScanner_SkipsBlankLines(t *testing.T) {
	want := []string{"Alice", "Bob"}
	rows := 0
	err := scanEach(t, "Alice,$1.00\n\n\r\nBob,$2.00\n", nil, func(a Account) {
		if rows < len(want) && a.Name != want[rows] {
			t.Errorf("row %d: expected %q, got %q", rows, want[rows], a.Name)
		}
		rows++
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows != 2 {
		t.Fatalf("expected exactly 2 rows, got %d", rows)
	}
}

func TestCSVScanner_KeepsWhitespaceOnlyLine(t *testing.T) {
	rows := 0
	err := scanEach(t, "Alice,note\n   \nBob,other\n", nil, func(n NoteRow) {
		if rows == 1 && n.Name != "   " {
			t.Errorf("expected the whitespace line to be kept as a cell, got %q", n.Name)
		}
		rows++
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows != 3 {
		t.Fatalf("expected the whitespace line to be a one-field record, got %d rows", rows)
	}
}

func TestCSVScanner_HeaderSkip(t *testing.T) {
	rows := 0
	err := scanEach(t, "name,balance\nAlice,$1.00\n", []csv.Option{csv.WithHeader()}, func(a Account) {
		if a.Name != "Alice" {
			t.Errorf("expected only Alice, got %q", a.Name)
		}
		rows++
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows != 1 {
		t.Fatalf("expected 1 row, got %d", rows)
	}
}

func TestCSVScanner_CustomDelimiter(t *testing.T) {
	var balances []int64
	rows := 0
	err := scanEach(t, "Alice;\"$1,250.00\"\n", []csv.Option{csv.WithDelimiter(';')}, func(a Account) {
		if a.Name != "Alice" {
			t.Errorf("expected Alice, got %q", a.Name)
		}
		balances = append(balances, a.Balance)
		rows++
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows != 1 {
		t.Fatalf("expected 1 row, got %d", rows)
	}
	if len(balances) != 1 || balances[0] != 125000 {
		t.Fatalf("unexpected balances: %v", balances)
	}
}

func TestCSVScanner_IgnoresInvalidDelimiter(t *testing.T) {
	for _, delim := range []byte{0, '"', '\n', '\r'} {
		rows := 0
		err := scanEach(t, "Alice,$1.00\n", []csv.Option{csv.WithDelimiter(delim)}, func(a Account) {
			if a.Name != "Alice" {
				t.Errorf("delimiter %q: expected default delimiter to stay active, got %q", delim, a.Name)
			}
			rows++
		})
		if err != nil {
			t.Fatalf("delimiter %q: unexpected error: %v", delim, err)
		}
		if rows != 1 {
			t.Fatalf("delimiter %q: expected 1 row, got %d", delim, rows)
		}
	}
}

func TestCSVScanner_MultilineQuotedField(t *testing.T) {
	rows := 0
	err := scanEach(t, "Alice,\"line one\nline two\"\n", nil, func(n NoteRow) {
		if n.Note != "line one\nline two" {
			t.Errorf("unexpected note: %q", n.Note)
		}
		rows++
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows != 1 {
		t.Fatalf("expected 1 row, got %d", rows)
	}
}

func TestCSVScanner_EscapedQuotes(t *testing.T) {
	rows := 0
	err := scanEach(t, "Alice,\"say \"\"hi\"\"\"\n", nil, func(n NoteRow) {
		if n.Note != `say "hi"` {
			t.Errorf("unexpected note: %q", n.Note)
		}
		rows++
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows != 1 {
		t.Fatalf("expected 1 row, got %d", rows)
	}
}

func TestCSVScanner_LastRecordWithoutNewline(t *testing.T) {
	rows := 0
	err := scanEach(t, "Alice,$1.00", nil, func(a Account) {
		if a.Name != "Alice" {
			t.Errorf("expected Alice, got %q", a.Name)
		}
		rows++
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows != 1 {
		t.Fatalf("expected 1 row, got %d", rows)
	}
}

func TestCSVScanner_RecordTooLarge(t *testing.T) {
	rows := 0
	err := scanEach(t, "Alice,$1.00\n", []csv.Option{csv.WithMaxRecordBytes(8)}, func(Account) {
		rows++
	})
	if !errors.Is(err, csv.ErrRecordTooLarge) {
		t.Fatalf("expected ErrRecordTooLarge, got %v", err)
	}
	if rows != 0 {
		t.Fatalf("expected zero rows, got %d", rows)
	}
}

func TestCSVScanner_MaxRecordDisabled(t *testing.T) {
	note := strings.Repeat("y", 256)
	rows := 0
	err := scanEach(t, "Alice,"+note+"\n", []csv.Option{csv.WithMaxRecordBytes(0)}, func(n NoteRow) {
		if n.Note != note {
			t.Errorf("expected the long record to parse, got %d bytes", len(n.Note))
		}
		rows++
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows != 1 {
		t.Fatalf("expected 1 row, got %d", rows)
	}
}

func TestCSVScanner_UnterminatedQuote(t *testing.T) {
	rows := 0
	err := scanEach(t, "Alice,\"oops\n", nil, func(Account) {
		rows++
	})
	if !errors.Is(err, csv.ErrUnterminatedQuote) {
		t.Fatalf("expected ErrUnterminatedQuote, got %v", err)
	}
	if rows != 0 {
		t.Fatalf("expected zero rows, got %d", rows)
	}
}

func TestCSVScanner_StripsBOMOnlyOnce(t *testing.T) {
	row := 0
	err := scanEach(t, "\xef\xbb\xbfAlice,$1.00\n\xef\xbb\xbfBob,$2.00\n", nil, func(a Account) {
		hasBOM := strings.HasPrefix(a.Name, "\xef\xbb\xbf")
		if row == 0 && hasBOM {
			t.Errorf("first record kept its BOM: %q", a.Name)
		}
		if row == 1 && !hasBOM {
			t.Errorf("second record BOM was stripped: %q", a.Name)
		}
		row++
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if row != 2 {
		t.Fatalf("expected 2 rows, got %d", row)
	}
}

func TestCSVScanner_ErrorIsSticky(t *testing.T) {
	scanner := csv.NewScanner(strings.NewReader("Alice,\"oops\n"))
	if scanner.Next() {
		t.Fatal("expected Next to fail on an unterminated quote")
	}
	if !errors.Is(scanner.Err(), csv.ErrUnterminatedQuote) {
		t.Fatalf("expected ErrUnterminatedQuote, got %v", scanner.Err())
	}
	for i := 0; i < 3; i++ {
		if scanner.Next() {
			t.Fatalf("Next returned true after the error was set")
		}
	}
}

func TestCSVScanner_MalformedTrailingQuote(t *testing.T) {
	rows := 0
	err := scanEach(t, "Alice,\"x\"y\n", nil, func(Account) {
		rows++
	})
	if !errors.Is(err, csv.ErrUnterminatedQuote) {
		t.Fatalf("expected ErrUnterminatedQuote, got %v", err)
	}
	if rows != 0 {
		t.Fatalf("expected zero rows, got %d", rows)
	}
}

func TestCSVScanner_MalformedInputIsParseError(t *testing.T) {
	cases := []struct {
		name string
		data string
		opts []csv.Option
	}{
		{
			name: "unterminated quote",
			data: "Alice,\"oops\n",
		},
		{
			name: "stray bytes after quote",
			data: "Alice,\"x\"y\n",
		},
		{
			name: "record too large",
			data: "Alice,$1.00\n",
			opts: []csv.Option{csv.WithMaxRecordBytes(8)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := stencil.Compile[Account]()
			if err != nil {
				t.Fatal(err)
			}

			scanner := csv.NewScanner(strings.NewReader(tc.data), tc.opts...)
			var events []stencil.RowError
			for _, rowErr := range stencil.Scan[Account](context.Background(), scanner, s) {
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
			if pd := events[0].ProblemDetails(); pd.Status != 400 {
				t.Errorf("expected status 400, got %d", pd.Status)
			}
			if scanner.Err() == nil {
				t.Error("expected scanner.Err() to keep the cause")
			}
		})
	}
}

func TestCSVScanner_NilOptionIsIgnored(t *testing.T) {
	s, err := stencil.Compile[Account]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := csv.NewScanner(strings.NewReader("Alice,$1.00\n"), nil)
	var rows int
	for _, rowErr := range stencil.Scan[Account](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatalf("unexpected row error: %+v", rowErr)
		}
		rows++
	}
	if rows != 1 {
		t.Fatalf("expected 1 row, got %d", rows)
	}
}

func TestCSVScanner_MaxRecordExactBoundary(t *testing.T) {
	s, err := stencil.Compile[Account]()
	if err != nil {
		t.Fatal(err)
	}

	ok := csv.NewScanner(strings.NewReader("Alice,1"), csv.WithMaxRecordBytes(7))
	var rows int
	for _, rowErr := range stencil.Scan[Account](context.Background(), ok, s) {
		if rowErr.Code != "" {
			t.Fatalf("unexpected row error: %+v", rowErr)
		}
		rows++
	}
	if rows != 1 {
		t.Fatalf("expected 1 row at the exact cap, got %d", rows)
	}

	over := csv.NewScanner(strings.NewReader("Alice,10"), csv.WithMaxRecordBytes(7))
	for _, rowErr := range stencil.Scan[Account](context.Background(), over, s) {
		if rowErr.Code != "" {
			break
		}
	}
	if !errors.Is(over.Err(), csv.ErrRecordTooLarge) {
		t.Fatalf("expected ErrRecordTooLarge one byte over the cap, got %v", over.Err())
	}
}

// scanEach streams data through the value-based Scan API and visits every
// decoded row in place. The visitor must not retain strings: they alias the
// scanner buffer and are only valid for the duration of the call.
func scanEach[T any](t *testing.T, data string, opts []csv.Option, fn func(T)) error {
	t.Helper()
	s, err := stencil.Compile[T]()
	if err != nil {
		t.Fatal(err)
	}
	scanner := csv.NewScanner(strings.NewReader(data), opts...)
	for item, rowErr := range stencil.Scan[T](context.Background(), scanner, s) {
		if rowErr.Code == "" {
			fn(item)
			continue
		}
		if err := scanner.Err(); err != nil {
			return err
		}
		return rowErr
	}
	return scanner.Err()
}
