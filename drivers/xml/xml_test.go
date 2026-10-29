package xml_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xdvi/stencil"
	"github.com/xdvi/stencil/drivers/xml"
)

type ItemXML struct {
	SKU   string `stencil:"key:sku,required"`
	Price int64  `stencil:"key:price,currency"`
}

func TestXML_Driver_Scan(t *testing.T) {
	data := `<catalog>
		<item><sku>SKU-A</sku><price>$15.00</price></item>
		<item><sku>SKU-B</sku><price>45.50</price></item>
	</catalog>`

	s, err := stencil.Compile[ItemXML]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := xml.NewScanner(strings.NewReader(data), "item")
	count := 0
	for item, rowErr := range stencil.Scan[ItemXML](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatal(rowErr)
		}
		count++
		if count == 1 && (item.SKU != "SKU-A" || item.Price != 1500) {
			t.Errorf("unexpected item: %+v", item)
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 items, got %d", count)
	}
}

func TestXML_Driver_EmptyItemTagFailsFast(t *testing.T) {
	scanner := xml.NewScanner(strings.NewReader("<items/>"), "")

	if scanner.Next() {
		t.Fatal("expected Next to return false for an empty item tag")
	}
	if !errors.Is(scanner.Err(), xml.ErrEmptyItemTag) {
		t.Fatalf("expected ErrEmptyItemTag, got %v", scanner.Err())
	}
}

func TestXML_Driver_MalformedDocumentIsParseError(t *testing.T) {
	s, err := stencil.Compile[ItemXML]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := xml.NewScanner(strings.NewReader(`<catalog><item><sku>SKU-A</sku></item>`), "item")

	var events []stencil.RowError
	valid := 0
	for _, rowErr := range stencil.Scan[ItemXML](context.Background(), scanner, s) {
		if rowErr.Code == "" {
			valid++
			continue
		}
		events = append(events, rowErr)
	}

	if scanner.Err() == nil {
		t.Fatal("expected scanner.Err() to be non-nil for malformed XML")
	}
	if scanner.Next() {
		t.Error("expected Next to stay false after a parse failure")
	}
	if valid != 1 {
		t.Errorf("expected the complete item before EOF to decode, got %d", valid)
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
}

func TestXML_Driver_TruncatedDocumentIsParseError(t *testing.T) {
	s, err := stencil.Compile[ItemXML]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := xml.NewScanner(strings.NewReader(`<catalog><item><sku>SKU-A`), "item")

	var events []stencil.RowError
	for _, rowErr := range stencil.Scan[ItemXML](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			events = append(events, rowErr)
		}
	}

	if scanner.Err() == nil {
		t.Fatal("expected scanner.Err() to be non-nil for truncated XML")
	}
	if len(events) != 1 || events[0].Code != "parse_error" {
		t.Fatalf("expected one parse_error event, got %+v", events)
	}
	if pd := events[0].ProblemDetails(); pd.Status != 400 {
		t.Errorf("expected status 400, got %d", pd.Status)
	}
}

func TestXML_Driver_EmptyItemTagReportsStreamError(t *testing.T) {
	s, err := stencil.Compile[ItemXML]()
	if err != nil {
		t.Fatal(err)
	}

	scanner := xml.NewScanner(strings.NewReader("<items/>"), "")

	var events []stencil.RowError
	for _, rowErr := range stencil.Scan[ItemXML](context.Background(), scanner, s) {
		events = append(events, rowErr)
	}

	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event, got %d: %+v", len(events), events)
	}
	if events[0].Code != "stream_error" {
		t.Errorf("expected stream_error, got %+v", events[0])
	}
	if !strings.Contains(events[0].Message, "item tag is empty") {
		t.Errorf("expected message to mention the empty item tag, got %q", events[0].Message)
	}
}
