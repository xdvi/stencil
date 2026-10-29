package xlsx_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/xdvi/stencil/drivers/xlsx"
)

func TestHybridSpool_RAMOnly(t *testing.T) {
	data := bytes.Repeat([]byte("spreadsheet-content-test-data\n"), 100)
	spool, err := xlsx.NewHybridSpoolReaderAt(bytes.NewReader(data), xlsx.WithSpoolRAMThreshold(1024*1024))
	if err != nil {
		t.Fatalf("failed to create spool: %v", err)
	}
	defer spool.Close()

	if spool.IsOnDisk() {
		t.Errorf("expected RAM-only spool, got on disk")
	}
	if spool.TempPath() != "" {
		t.Errorf("expected empty temp path, got %s", spool.TempPath())
	}
	if spool.Size() != int64(len(data)) {
		t.Errorf("expected size %d, got %d", len(data), spool.Size())
	}

	// Test Read
	readBuf := make([]byte, len(data))
	n, err := io.ReadFull(spool, readBuf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if n != len(data) || !bytes.Equal(readBuf, data) {
		t.Errorf("data read mismatch")
	}

	// Test Seek & ReadAt
	if _, err := spool.Seek(10, io.SeekStart); err != nil {
		t.Fatalf("seek failed: %v", err)
	}
	peek := make([]byte, 5)
	if _, err := spool.ReadAt(peek, 0); err != nil {
		t.Fatalf("readat failed: %v", err)
	}
	if !bytes.Equal(peek, data[:5]) {
		t.Errorf("readat mismatch: expected %q, got %q", data[:5], peek)
	}
}

func TestHybridSpool_SpillToDisk(t *testing.T) {
	// Small 512-byte RAM threshold
	ramLimit := int64(512)
	data := bytes.Repeat([]byte("abcdefghijklmnopqrstuvwxyz012345"), 50) // 1600 bytes > 512

	spool, err := xlsx.NewHybridSpoolReaderAt(bytes.NewReader(data), xlsx.WithSpoolRAMThreshold(ramLimit))
	if err != nil {
		t.Fatalf("failed to create spool: %v", err)
	}

	if !spool.IsOnDisk() {
		t.Errorf("expected spool to spill to disk")
	}
	tmpPath := spool.TempPath()
	if tmpPath == "" {
		t.Fatalf("expected non-empty temp path")
	}
	if _, err := os.Stat(tmpPath); err != nil {
		t.Fatalf("temp file does not exist on disk: %v", err)
	}

	readBuf := make([]byte, len(data))
	n, err := io.ReadFull(spool, readBuf)
	if err != nil {
		t.Fatalf("read disk spool failed: %v", err)
	}
	if n != len(data) || !bytes.Equal(readBuf, data) {
		t.Errorf("disk spool read data mismatch")
	}

	// Verify cleanup
	if err := spool.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("temp file was not deleted upon Close: %v", err)
	}
}

func TestHybridSpool_FileTooLarge(t *testing.T) {
	data := []byte("payload exceeds max bytes")
	_, err := xlsx.NewHybridSpoolReaderAt(
		bytes.NewReader(data),
		xlsx.WithSpoolRAMThreshold(1024),
		xlsx.WithSpoolMaxBytes(10),
	)
	if !errors.Is(err, xlsx.ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestHybridSpool_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-canceled

	_, err := xlsx.NewHybridSpoolReaderAt(
		bytes.NewReader([]byte("some data")),
		xlsx.WithSpoolContext(ctx),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func BenchmarkHybridSpool_RAM(b *testing.B) {
	payload := bytes.Repeat([]byte("test row content\n"), 1000) // ~17 KB
	r := bytes.NewReader(payload)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r.Reset(payload)
		spool, err := xlsx.NewHybridSpoolReaderAt(r, xlsx.WithSpoolRAMThreshold(64*1024))
		if err != nil {
			b.Fatal(err)
		}
		_ = spool.Close()
	}
}
