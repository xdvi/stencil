package stencil

import (
	"context"
	"iter"
	"unsafe"
)

// Scan decodes records from scanner using pre-compiled schema s in a zero-alloc streaming loop.
//
// Scan is the zero-allocation path: the yielded record and any strings it contains
// are only valid until the next iteration. Retain a record only after copying its
// string fields; use ScanCopy to have stencil perform that copy.
func Scan[T any](ctx context.Context, scanner RowScanner, s *Schema[T]) iter.Seq2[T, RowError] {
	return scan(ctx, scanner, s, false)
}

// ScanCopy decodes records like Scan but yields deep-copied rows.
//
// String fields are cloned before each yield, so the yielded record is safe to
// retain after the loop. time.Time values point only to immutable location data
// and need no additional copy. Use Scan when records are consumed in place.
func ScanCopy[T any](ctx context.Context, scanner RowScanner, s *Schema[T]) iter.Seq2[T, RowError] {
	return scan(ctx, scanner, s, true)
}

func scan[T any](ctx context.Context, scanner RowScanner, s *Schema[T], copyRows bool) iter.Seq2[T, RowError] {
	return func(yield func(T, RowError) bool) {
		var item T
		var zero T
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
				yield(zero, RowError{Row: rowNum, Code: "scanner_error", Message: err.Error()})
				return
			}

			if hasError {
				if !yield(zero, rowErr) {
					return
				}
				continue
			}

			reqErrs := s.ValidateRequired(&item, rowNum, errBuf)
			if len(reqErrs) > 0 {
				for _, re := range reqErrs {
					if !yield(zero, re) {
						return
					}
				}
				continue
			}

			if copyRows {
				s.detachAll(&item)
			}

			if !yield(item, RowError{}) {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			yield(zero, RowError{Row: rowNum, Code: "stream_error", Message: err.Error()})
		}
	}
}

func (s *Schema[T]) detachAll(target *T) {
	base := unsafe.Pointer(target)
	for i := range s.all {
		s.all[i].Detach(base)
	}
}
