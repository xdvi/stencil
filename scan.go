package stencil

import (
	"context"
	"iter"
)

// Scan decodes records from scanner using pre-compiled schema s in a zero-alloc streaming loop.
func Scan[T any](ctx context.Context, scanner RowScanner, s *Schema[T]) iter.Seq2[*T, RowError] {
	return func(yield func(*T, RowError) bool) {
		var item T
		rowNum := 0
		var rowErr RowError
		errBuf := make([]RowError, 0, 8)

		for scanner.Next() {
			if err := ctx.Err(); err != nil {
				return
			}
			rowNum++
			s.ResetAndBind(&item)

			hasError := false
			err := scanner.ScanFields(func(col int, key []byte, val []byte) bool {
				if !s.SetField(&item, rowNum, col, key, val, &rowErr) {
					hasError = true
					return false
				}
				return true
			})

			if err != nil {
				yield(nil, RowError{Row: rowNum, Code: "scanner_error", Message: err.Error()})
				return
			}

			if hasError {
				if !yield(nil, rowErr) {
					return
				}
				continue
			}

			reqErrs := s.ValidateRequired(&item, rowNum, errBuf)
			if len(reqErrs) > 0 {
				for _, re := range reqErrs {
					if !yield(nil, re) {
						return
					}
				}
				continue
			}

			if !yield(&item, RowError{}) {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			yield(nil, RowError{Row: rowNum, Code: "stream_error", Message: err.Error()})
		}
	}
}
