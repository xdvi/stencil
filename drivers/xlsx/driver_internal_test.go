package xlsx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/xuri/excelize/v2"
)

func buildXLSX(t *testing.T) []byte {
	t.Helper()

	f := excelize.NewFile()
	defer f.Close()

	sw, err := f.NewStreamWriter("Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sw.SetRow("A1", []interface{}{"SKU-001", "19.99"}); err != nil {
		t.Fatal(err)
	}
	if err := sw.SetRow("A2", []interface{}{"SKU-002", "49.50"}); err != nil {
		t.Fatal(err)
	}
	if err := sw.Flush(); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestXLSXScanner_CloseRemovesTempFile(t *testing.T) {
	s, err := NewScanner(bytes.NewReader(buildXLSX(t)), "Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	path := s.tmpFile
	if path == "" {
		t.Fatal("expected a recorded temp file path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("temp file missing before Close: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temp file still present after Close: %v", err)
	}
	if s.tmpFile != "" {
		t.Fatalf("tmpFile not cleared after Close: %q", s.tmpFile)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestXLSXScanner_NextWithStoredError(t *testing.T) {
	s, err := NewScanner(bytes.NewReader(buildXLSX(t)), "Sheet1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.err = errors.New("stored failure")
	if s.Next() {
		t.Fatal("Next returned true with a stored error")
	}
}

func TestXLSXScanner_NewScannerRemovesTempFileOnFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	assertEmptyTempDir := func(t *testing.T) {
		t.Helper()
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("expected no temp files, found %d", len(entries))
		}
	}

	t.Run("too large", func(t *testing.T) {
		_, err := NewScanner(bytes.NewReader(buildXLSX(t)), "Sheet1", WithMaxBytes(64))
		if !errors.Is(err, ErrFileTooLarge) {
			t.Fatalf("expected ErrFileTooLarge, got %v", err)
		}
		assertEmptyTempDir(t)
	})

	t.Run("canceled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := NewScanner(bytes.NewReader(buildXLSX(t)), "Sheet1", WithContext(ctx))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		assertEmptyTempDir(t)
	})
}
