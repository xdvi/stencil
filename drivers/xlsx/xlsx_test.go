package xlsx_test

import (
	"bytes"
	"context"
	"errors"
	"io"
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
	if err := scanner.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := scanner.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestXLSXScanner_NilReceiver(t *testing.T) {
	var s *xlsx.XLSXScanner
	if s.Next() {
		t.Fatal("Next on nil receiver returned true")
	}
	if err := s.ScanFields(func(int, []byte, []byte) bool { return true }); err != nil {
		t.Fatalf("ScanFields on nil receiver: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close on nil receiver: %v", err)
	}
	if err := s.Err(); err != nil {
		t.Fatalf("Err on nil receiver: %v", err)
	}
}

func TestXLSXScanner_AfterClose(t *testing.T) {
	scanner, err := xlsx.NewScanner(bytes.NewReader(createXLSX()), "Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	if err := scanner.Close(); err != nil {
		t.Fatal(err)
	}
	if scanner.Next() {
		t.Fatal("Next after Close returned true")
	}
	if err := scanner.ScanFields(func(int, []byte, []byte) bool { return true }); err != nil {
		t.Fatalf("ScanFields after Close: %v", err)
	}
}

func TestXLSXScanner_FileTooLarge(t *testing.T) {
	scanner, err := xlsx.NewScanner(bytes.NewReader(createXLSX()), "Sheet1", xlsx.WithMaxBytes(64))
	if scanner != nil {
		_ = scanner.Close()
		t.Fatal("expected nil scanner when input exceeds the size cap")
	}
	if !errors.Is(err, xlsx.ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestXLSXScanner_MaxBytesDisabled(t *testing.T) {
	scanner, err := xlsx.NewScanner(bytes.NewReader(createXLSX()), "Sheet1", xlsx.WithMaxBytes(0))
	if err != nil {
		t.Fatal(err)
	}
	defer scanner.Close()

	count := 0
	for scanner.Next() {
		if err := scanner.ScanFields(func(int, []byte, []byte) bool { return true }); err != nil {
			t.Fatal(err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("unexpected scanner error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 rows, got %d", count)
	}
}

func TestXLSXScanner_PreCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	scanner, err := xlsx.NewScanner(bytes.NewReader(createXLSX()), "Sheet1", xlsx.WithContext(ctx))
	if scanner != nil {
		_ = scanner.Close()
		t.Fatal("expected nil scanner for a canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestXLSXScanner_NilContext(t *testing.T) {
	scanner, err := xlsx.NewScanner(bytes.NewReader(createXLSX()), "Sheet1", xlsx.WithContext(nil))
	if err != nil {
		t.Fatal(err)
	}
	defer scanner.Close()

	if !scanner.Next() {
		t.Fatalf("expected at least one row, got %v", scanner.Err())
	}
}

func TestXLSXScanner_CancelDuringCopy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	src := &cancelAfterFirstChunk{
		cancel: cancel,
		data:   bytes.Repeat([]byte("x"), 1024),
	}

	scanner, err := xlsx.NewScanner(src, "Sheet1", xlsx.WithContext(ctx))
	if scanner != nil {
		_ = scanner.Close()
		t.Fatal("expected nil scanner when the context is canceled during copy")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// cancelAfterFirstChunk returns a small first chunk and cancels the context,
// leaving data unread so the next read observes the cancellation.
type cancelAfterFirstChunk struct {
	cancel   context.CancelFunc
	data     []byte
	offset   int
	canceled bool
}

func (r *cancelAfterFirstChunk) Read(p []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	end := min(r.offset+8, len(r.data))
	n := copy(p, r.data[r.offset:end])
	r.offset += n
	if !r.canceled {
		r.canceled = true
		r.cancel()
	}
	return n, nil
}
