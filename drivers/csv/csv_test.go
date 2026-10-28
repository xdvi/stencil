package csv_test

import (
	"context"
	"strings"
	"testing"

	"github.com/xdvi/stencil"
	"github.com/xdvi/stencil/drivers/csv"
)

type Account struct {
	Name    string `stencil:"col:0,required"`
	Balance int64  `stencil:"col:1,currency"`
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
