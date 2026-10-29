package json_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xdvi/stencil"
	"github.com/xdvi/stencil/drivers/json"
)

type UserJSON struct {
	Username string `stencil:"key:user,required"`
	Credits  int64  `stencil:"key:credits,currency"`
}

func TestJSON_Driver_ArrayStream(t *testing.T) {
	data := `[
		{"user": "alice", "credits": "$50.00"},
		{"user": "bob", "credits": "12.50"}
	]`

	s, err := stencil.Compile[UserJSON]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := json.NewScanner(strings.NewReader(data))
	count := 0
	for item, rowErr := range stencil.Scan[UserJSON](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatal(rowErr)
		}
		count++
		if count == 1 && (item.Username != "alice" || item.Credits != 5000) {
			t.Errorf("unexpected user 1: %+v", item)
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 items, got %d", count)
	}
}

func TestJSON_Driver_InvalidPayloadReportsStreamError(t *testing.T) {
	s, err := stencil.Compile[UserJSON]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := json.NewScanner(strings.NewReader(`not json`))

	var events []stencil.RowError
	for item, rowErr := range stencil.Scan[UserJSON](context.Background(), scanner, s) {
		if rowErr.Code == "" {
			t.Errorf("expected zero valid records, got %+v", item)
		}
		events = append(events, rowErr)
	}

	if scanner.Err() == nil {
		t.Fatal("expected scanner.Err() to be non-nil for invalid payload")
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	if events[0].Code != "stream_error" {
		t.Errorf("expected stream_error event, got %+v", events[0])
	}
}

func TestJSON_Driver_ErrorInNonLastKeyKeepsStreamPosition(t *testing.T) {
	data := `[{"credits":"oops","user":"b"},{"credits":"2.00","user":"c"}]`

	s, err := stencil.Compile[UserJSON]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := json.NewScanner(strings.NewReader(data))

	var events []stencil.RowError
	var items []UserJSON
	for item, rowErr := range stencil.Scan[UserJSON](context.Background(), scanner, s) {
		if rowErr.Code == "" {
			items = append(items, item)
		}
		events = append(events, rowErr)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %+v", len(events), events)
	}
	if events[0].Code != "invalid_currency" {
		t.Errorf("row 1: expected invalid_currency, got %+v", events[0])
	}
	for _, e := range events {
		if e.Code == "scanner_error" {
			t.Fatalf("stream desynchronized after row error: %+v", e)
		}
	}
	if events[1].Code != "" {
		t.Fatalf("row 2: expected valid row, got %+v", events[1])
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 valid item, got %d: %+v", len(items), items)
	}
	if items[0].Username != "c" || items[0].Credits != 200 {
		t.Errorf("unexpected row 2: %+v", items[0])
	}
}

type OptionalJSON struct {
	Username string    `stencil:"key:user"`
	Credits  int64     `stencil:"key:credits,currency"`
	When     time.Time `stencil:"key:when,iso8601"`
}

func TestJSON_Driver_DecodesEscapedStrings(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{name: "quote", data: `[{"user":"a\"b","credits":"1.00"}]`, want: `a"b`},
		{name: "unicode", data: `[{"user":"caf\u00e9","credits":"1.00"}]`, want: "café"},
		{name: "backslash and newline", data: `[{"user":"a\\b\nc","credits":"1.00"}]`, want: "a\\b\nc"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := stencil.Compile[UserJSON]()
			if err != nil {
				t.Fatal(err)
			}

			scanner := json.NewScanner(strings.NewReader(tc.data))
			var items []UserJSON
			for item, rowErr := range stencil.Scan[UserJSON](context.Background(), scanner, s) {
				if rowErr.Code != "" {
					t.Fatalf("unexpected row error: %+v", rowErr)
				}
				items = append(items, item)
			}

			if scanner.Err() != nil {
				t.Fatalf("unexpected scanner error: %v", scanner.Err())
			}
			if len(items) != 1 {
				t.Fatalf("expected 1 item, got %d", len(items))
			}
			if items[0].Username != tc.want {
				t.Errorf("expected %q, got %q", tc.want, items[0].Username)
			}
		})
	}
}

func TestJSON_Driver_NullValuesBecomeEmpty(t *testing.T) {
	s, err := stencil.Compile[OptionalJSON]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := json.NewScanner(strings.NewReader(`[{"user":null,"credits":null,"when":null}]`))

	var items []OptionalJSON
	var events []stencil.RowError
	for item, rowErr := range stencil.Scan[OptionalJSON](context.Background(), scanner, s) {
		events = append(events, rowErr)
		if rowErr.Code == "" {
			items = append(items, item)
		}
	}

	if scanner.Err() != nil {
		t.Fatalf("unexpected scanner error: %v", scanner.Err())
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d: %+v", len(items), events)
	}
	if items[0].Username != "" {
		t.Errorf("expected empty username, got %q", items[0].Username)
	}
	if items[0].Credits != 0 {
		t.Errorf("expected zero credits, got %d", items[0].Credits)
	}
	if !items[0].When.IsZero() {
		t.Errorf("expected zero time, got %v", items[0].When)
	}
	for _, e := range events {
		if e.Code != "" {
			t.Fatalf("unexpected event for null values: %+v", e)
		}
	}
}

func TestJSON_Driver_NullRequiredFieldReportsRequired(t *testing.T) {
	s, err := stencil.Compile[UserJSON]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := json.NewScanner(strings.NewReader(`[{"user":null,"credits":null}]`))

	var events []stencil.RowError
	for _, rowErr := range stencil.Scan[UserJSON](context.Background(), scanner, s) {
		events = append(events, rowErr)
	}

	if scanner.Err() != nil {
		t.Fatalf("unexpected scanner error: %v", scanner.Err())
	}
	if len(events) != 1 || events[0].Code != "required_field" {
		t.Fatalf("expected one required_field event, got %+v", events)
	}
}

func TestJSON_Driver_NestedValuesAreRejected(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{name: "object", data: `[{"user":{"a":1}}]`},
		{name: "array", data: `[{"user":["a"]}]`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := stencil.Compile[UserJSON]()
			if err != nil {
				t.Fatal(err)
			}

			scanner := json.NewScanner(strings.NewReader(tc.data))

			var events []stencil.RowError
			for _, rowErr := range stencil.Scan[UserJSON](context.Background(), scanner, s) {
				events = append(events, rowErr)
			}

			if scanner.Err() == nil {
				t.Fatal("expected scanner.Err() to be non-nil for nested value")
			}
			if !strings.Contains(scanner.Err().Error(), "nested") {
				t.Errorf("expected error to mention nested, got %v", scanner.Err())
			}
			if len(events) != 1 || events[0].Code != "scanner_error" {
				t.Fatalf("expected one scanner_error event, got %+v", events)
			}
		})
	}
}

func TestJSON_Driver_NonArrayRootIsRejected(t *testing.T) {
	cases := []struct {
		data  string
		token string
	}{
		{data: `null`, token: "null"},
		{data: `42`, token: "42"},
		{data: `"x"`, token: "x"},
		{data: `{}`, token: "{"},
		{data: `true`, token: "true"},
	}

	for _, tc := range cases {
		t.Run(tc.data, func(t *testing.T) {
			s, err := stencil.Compile[UserJSON]()
			if err != nil {
				t.Fatal(err)
			}

			scanner := json.NewScanner(strings.NewReader(tc.data))

			var events []stencil.RowError
			valid := 0
			for _, rowErr := range stencil.Scan[UserJSON](context.Background(), scanner, s) {
				events = append(events, rowErr)
				if rowErr.Code == "" {
					valid++
				}
			}

			if valid != 0 {
				t.Errorf("expected zero valid rows, got %d", valid)
			}
			if scanner.Err() == nil {
				t.Fatal("expected scanner.Err() to be non-nil for non-array root")
			}
			if !strings.Contains(scanner.Err().Error(), "[") || !strings.Contains(scanner.Err().Error(), tc.token) {
				t.Errorf("expected error to mention '[' and %q, got %v", tc.token, scanner.Err())
			}
			if len(events) != 1 || events[0].Code != "stream_error" {
				t.Fatalf("expected one stream_error event, got %+v", events)
			}
			if scanner.Next() {
				t.Error("expected Next to stay false after a fatal error")
			}
		})
	}
}

func TestJSON_Driver_NextAfterArrayClose(t *testing.T) {
	scanner := json.NewScanner(strings.NewReader(`[{"user":"a","credits":"1.00"}]`))

	if !scanner.Next() {
		t.Fatalf("expected first row, got error: %v", scanner.Err())
	}
	if err := scanner.ScanFields(func(col int, key []byte, val []byte) bool { return true }); err != nil {
		t.Fatalf("unexpected field error: %v", err)
	}
	if scanner.Next() {
		t.Error("expected Next to be false after the array closes")
	}
	if scanner.Err() != nil {
		t.Errorf("unexpected scanner error: %v", scanner.Err())
	}
}

func TestJSON_Driver_EmptyInputIsCleanEOF(t *testing.T) {
	s, err := stencil.Compile[UserJSON]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := json.NewScanner(strings.NewReader(""))

	var events []stencil.RowError
	for _, rowErr := range stencil.Scan[UserJSON](context.Background(), scanner, s) {
		events = append(events, rowErr)
	}

	if scanner.Err() != nil {
		t.Fatalf("expected clean EOF, got %v", scanner.Err())
	}
	if len(events) != 0 {
		t.Fatalf("expected no events for empty input, got %+v", events)
	}
}
