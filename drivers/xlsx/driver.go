package xlsx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/xdvi/stencil"
	"github.com/xuri/excelize/v2"
)

// ErrFileTooLarge is returned when the source exceeds the configured byte cap.
var ErrFileTooLarge = errors.New("xlsx: file exceeds maximum size")

const defaultMaxBytes int64 = 512 << 20

type options struct {
	maxBytes int64
	ctx      context.Context
}

// Option configures a scanner created by NewScanner.
type Option func(*options)

// WithMaxBytes caps how many bytes are copied from the source. A value of zero
// or less disables the cap.
func WithMaxBytes(n int64) Option {
	return func(o *options) {
		if n > 0 {
			o.maxBytes = n
			return
		}
		o.maxBytes = 0
	}
}

// WithContext associates ctx with the copy phase. A nil context falls back to
// context.Background.
func WithContext(ctx context.Context) Option {
	return func(o *options) {
		if ctx == nil {
			ctx = context.Background()
		}
		o.ctx = ctx
	}
}

// XLSXScanner streams XLSX rows one by one through a temporary disk buffer to preserve a flat memory footprint.
type XLSXScanner struct {
	tmpFile string
	file    *excelize.File
	rows    *excelize.Rows
	err     error
	closed  bool
}

// NewScanner buffers r to a temporary file on disk and opens it via excelize.OpenFile.
func NewScanner(r io.Reader, sheet string, opts ...Option) (*XLSXScanner, error) {
	o := options{maxBytes: defaultMaxBytes, ctx: context.Background()}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	tmp, err := os.CreateTemp("", "stencil-xlsx-*.xlsx")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()

	src := &contextReader{ctx: o.ctx, r: r}
	var reader io.Reader = src
	if o.maxBytes > 0 && o.maxBytes < math.MaxInt64 {
		reader = io.LimitReader(src, o.maxBytes+1)
	}

	n, err := io.Copy(tmp, reader)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return nil, err
	}
	if o.maxBytes > 0 && n > o.maxBytes {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return nil, ErrFileTooLarge
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return nil, err
	}

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
	if s == nil || s.closed || s.err != nil || s.rows == nil {
		return false
	}
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
	if s == nil || s.closed || s.rows == nil {
		return nil
	}
	cols, err := s.rows.Columns()
	if err != nil {
		s.err = err
		return err
	}
	for i, c := range cols {
		val := []byte(c)
		if !yield(i, nil, val) {
			break
		}
	}
	return nil
}

// Err returns the first non-EOF error encountered.
func (s *XLSXScanner) Err() error {
	if s == nil {
		return nil
	}
	return s.err
}

// Close closes open file handles and cleans up the temporary disk file.
// It is safe to call multiple times; subsequent calls return nil.
func (s *XLSXScanner) Close() error {
	if s == nil || s.closed {
		return nil
	}
	s.closed = true

	var firstErr error
	if s.rows != nil {
		if err := s.rows.Close(); err != nil {
			firstErr = err
		}
	}
	if s.file != nil {
		if err := s.file.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.tmpFile != "" {
		if err := os.Remove(s.tmpFile); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
		s.tmpFile = ""
	}
	return firstErr
}

// contextReader aborts a read as soon as ctx is canceled.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
