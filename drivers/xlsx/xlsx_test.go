package xlsx_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/xdvi/stencil"
	"github.com/xdvi/stencil/drivers/xlsx"
	"github.com/xuri/excelize/v2"
)

type Product struct {
	SKU   string `stencil:"col:0,required"`
	Price int64  `stencil:"col:1,currency"`
}

func createXLSX() []byte {
	f := excelize.NewFile()
	defer f.Close()
	sw, _ := f.NewStreamWriter("Sheet1")
	_ = sw.SetRow("A1", []interface{}{"SKU-001", "19.99"})
	_ = sw.SetRow("A2", []interface{}{"SKU-002", "49.50"})
	_ = sw.Flush()
	var buf bytes.Buffer
	_ = f.Write(&buf)
	return buf.Bytes()
}

func TestXLSX_Driver_Scan(t *testing.T) {
	data := createXLSX()
	s, err := stencil.Compile[Product]()
	if err != nil {
		t.Fatal(err)
	}

	scanner, err := xlsx.NewScanner(bytes.NewReader(data), "Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	defer scanner.Close()

	count := 0
	for item, rowErr := range stencil.Scan[Product](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatal(rowErr)
		}
		count++
		if count == 1 && item.Price != 1999 {
			t.Errorf("expected 1999, got %d", item.Price)
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 items, got %d", count)
	}
}
