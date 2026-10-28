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
