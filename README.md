# stencil

[![CI](https://github.com/xdvi/stencil/actions/workflows/ci.yml/badge.svg)](https://github.com/xdvi/stencil/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/xdvi/stencil.svg)](https://pkg.go.dev/github.com/xdvi/stencil)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

High-performance, zero-allocation streaming tabular data micro-framework for Go. Unifies import and export of **CSV**, **JSON**, **XML**, **XLSX**, and custom tabular/document formats under pre-compiled, reflection-free schemas.

---

## Highlights

- **Extreme Memory Efficiency**: Constant memory ceiling. Processes **100,000 rows with < 1 MB HeapInuse** (verified 0.68 MB).
- **Zero-Allocation Hot Path**: Pre-compiled schema offsets and reusable row binders eliminate heap allocations inside streaming loops.
- **True OCP Extensibility**: The core knows nothing about file formats. Any format is simply a low-level `RowScanner` token visitor.
- **Strict International Standards**:
  - **ISO 8601 / RFC 3339**: 24ns zero-alloc parser with calendar-accurate month/leap-year validation.
  - **ISO 4217**: Monetary amounts parsed directly to `int64` minor units (cents) with overflow guards (`float64` strictly forbidden).
  - **RFC 9457**: Row-level errors emit standard `application/problem+json` documents.
- **Native Go 1.23 Iterators**: Clean pull-based streaming using `iter.Seq2[T, RowError]`.

---

## Benchmark Results

```text
BenchmarkParseISO8601_ZeroAlloc-8   45,414,752   24.49 ns/op    0 B/op    0 allocs/op
BenchmarkSetField_ZeroAlloc-8      215,005,142    5.81 ns/op    0 B/op    0 allocs/op
Test100kRows_MemoryCeiling_Under20MB: Processed 100,000 rows in 0.02s (HeapInuse: 0.68 MB, TotalAllocDiff: 0.07 MB)
```

---

## Installation

```bash
go get github.com/xdvi/stencil
```

---

## Quick Start

### 1. Define Unified Struct Schema

Use struct tags to define column indexes (for positional formats like CSV/XLSX) and keys (for named formats like JSON/XML):

```go
package main

import (
    "time"
    "github.com/xdvi/stencil"
)

type Invoice struct {
    ID        string    `stencil:"col:0,key:id,required"`
    Amount    int64     `stencil:"col:1,key:amount,currency"` // Stored in cents ($12.50 -> 1250)
    CreatedAt time.Time `stencil:"col:2,key:created_at,iso8601"`
    Note      string    `stencil:"col:3,key:note"`
}
```

### 2. Stream Ingest (CSV / JSON / XML / XLSX)

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/xdvi/stencil"
    "github.com/xdvi/stencil/drivers/csv"
    "github.com/xdvi/stencil/drivers/json"
    "github.com/xdvi/stencil/drivers/xlsx"
    "github.com/xdvi/stencil/drivers/xml"
)

func main() {
    ctx := context.Background()

    // Compile schema once at startup
    s, err := stencil.Compile[Invoice]()
    if err != nil {
        panic(err)
    }

    file, _ := os.Open("invoices.csv")
    defer file.Close()

    scanner := csv.NewScanner(file)

    for record, rowErr := range stencil.Scan[Invoice](ctx, scanner, s) {
        if rowErr.Code != "" {
            // Emits RFC 9457 Problem Details
            fmt.Printf("Row error: %s\n", rowErr.ProblemDetails().Detail)
            continue
        }
        fmt.Printf("Invoice: %s, Amount: %d cents\n", record.ID, record.Amount)
    }
}
```

`Scan` yields each record by value and is the zero-allocation path: the record and its strings are only valid until the next iteration. If you need to retain rows, use `stencil.ScanCopy`, which clones string fields before yielding.

---

## Writing a Custom Driver in 30 Lines

Implement the low-level `RowScanner` primitive:

```go
type RowScanner interface {
    Next() bool
    ScanFields(yield func(col int, key []byte, val []byte) bool) error
    Err() error
}
```

Pass index-based formats as `(col, nil, val)` or key-based formats as `(-1, key, val)`. `stencil` handles all standard validations, memory reuse, and struct injection.

---

## License

MIT License. See [LICENSE](LICENSE) for details.
