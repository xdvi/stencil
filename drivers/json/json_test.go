package json_test

import (
	"context"
	"strings"
	"testing"

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
		if item != nil {
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
		if item != nil {
			items = append(items, *item)
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
