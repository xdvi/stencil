package perf_test

import (
	"context"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/xdvi/stencil"
	"github.com/xdvi/stencil/drivers/csv"
)

type BenchmarkRow struct {
	ID        string    `stencil:"col:0,key:id,required"`
	Amount    int64     `stencil:"col:1,key:amount,currency"`
	Timestamp time.Time `stencil:"col:2,key:time,iso8601"`
}

type infiniteCSVReader struct {
	totalRows int
	currRow   int
	line      []byte
	offset    int
}

func newInfiniteCSVReader(rows int) *infiniteCSVReader {
	return &infiniteCSVReader{
		totalRows: rows,
		line:      []byte("INV-999999,\"$1,250.75\",2026-10-28T12:00:00Z\n"),
	}
}

func (r *infiniteCSVReader) Read(p []byte) (n int, err error) {
	if r.currRow >= r.totalRows {
		return 0, io.EOF
	}

	written := 0
	for written < len(p) && r.currRow < r.totalRows {
		avail := len(r.line) - r.offset
		toCopy := len(p) - written
		if toCopy > avail {
			toCopy = avail
		}
		copy(p[written:], r.line[r.offset:r.offset+toCopy])
		written += toCopy
		r.offset += toCopy
		if r.offset >= len(r.line) {
			r.offset = 0
			r.currRow++
		}
	}
	return written, nil
}

func Test100kRows_MemoryCeiling_Under20MB(t *testing.T) {
	runtime.GC()
	var mBefore runtime.MemStats
	runtime.ReadMemStats(&mBefore)

	s, err := stencil.Compile[BenchmarkRow]()
	if err != nil {
		t.Fatal(err)
	}

	reader := newInfiniteCSVReader(100_000)
	scanner := csv.NewScanner(reader)

	processed := 0
	for _, rowErr := range stencil.Scan[BenchmarkRow](context.Background(), scanner, s) {
		if rowErr.Code != "" {
			t.Fatalf("unexpected error at row %d: %v", processed+1, rowErr)
		}
		processed++
	}

	if processed != 100_000 {
		t.Fatalf("expected 100,000 processed rows, got %d", processed)
	}

	var mAfter runtime.MemStats
	runtime.ReadMemStats(&mAfter)

	heapInuseMB := float64(mAfter.HeapInuse) / (1024 * 1024)
	totalAllocDiffMB := float64(int64(mAfter.TotalAlloc)-int64(mBefore.TotalAlloc)) / (1024 * 1024)

	if heapInuseMB > 20.0 {
		t.Fatalf("MEMORY CEILING EXCEEDED: HeapInuse is %.2f MB (limit 20MB)", heapInuseMB)
	}
	if totalAllocDiffMB > 1.0 {
		t.Fatalf("ZERO-ALLOC VIOLATION: 100k rows allocated %.2f MB cumulative heap (limit 1MB)", totalAllocDiffMB)
	}
	t.Logf("Success: 100,000 rows processed with HeapInuse: %.2f MB, TotalAllocDiff: %.2f MB", heapInuseMB, totalAllocDiffMB)
}
