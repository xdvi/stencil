package xml_test

import (
	"context"
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
