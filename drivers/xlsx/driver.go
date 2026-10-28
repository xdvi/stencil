package xlsx

import (
	"fmt"
	"io"
	"os"
	"unsafe"

	"github.com/xdvi/stencil"
	"github.com/xuri/excelize/v2"
)

// XLSXScanner streams XLSX rows one by one through a temporary disk buffer to preserve a flat memory footprint.
type XLSXScanner struct {
	tmpFile string
	file    *excelize.File
	rows    *excelize.Rows
	err     error
}

// NewScanner buffers r to a temporary file on disk and opens it via excelize.OpenFile.
func NewScanner(r io.Reader, sheet string) (*XLSXScanner, error) {
	tmp, err := os.CreateTemp("", "stencil-xlsx-*.xlsx")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()

	if _, err := io.Copy(tmp, r); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return nil, err
	}
	_ = tmp.Close()

	f, err := excelize.OpenFile(tmpName)
	if err != nil {
		_ = os.Remove(tmpName)
		return nil, err
	}

	rows, err := f.Rows(sheet)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(tmpName)
		return nil, fmt.Errorf("sheet %s not found: %w", sheet, err)
	}

	return &XLSXScanner{
		tmpFile: tmpName,
		file:    f,
		rows:    rows,
	}, nil
}

// Next advances to the next row in the sheet.
func (s *XLSXScanner) Next() bool {
	ok := s.rows.Next()
	if !ok {
		if err := s.rows.Error(); err != nil {
			s.err = err
		}
	}
	return ok
}

// ScanFields yields column index and cell bytes for the current row.
func (s *XLSXScanner) ScanFields(yield stencil.FieldVisitor) error {
	cols, err := s.rows.Columns()
	if err != nil {
		return err
	}
	for i, c := range cols {
		val := unsafe.Slice(unsafe.StringData(c), len(c))
		if !yield(i, nil, val) {
			break
		}
	}
	return nil
}

// Err returns the first non-EOF error encountered.
func (s *XLSXScanner) Err() error {
	return s.err
}

// Close closes open file handles and cleans up the temporary disk file.
func (s *XLSXScanner) Close() error {
	defer os.Remove(s.tmpFile)
	if s.rows != nil {
		_ = s.rows.Close()
	}
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}
