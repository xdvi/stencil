package csv

import (
	"bufio"
	"errors"
	"io"

	"github.com/xdvi/stencil"
)

var (
	// ErrRecordTooLarge reports a record that grew past the configured limit.
	ErrRecordTooLarge = errors.New("csv: record exceeds maximum size")
	// ErrUnterminatedQuote reports a quoted field left open at end of input, or
	// a record with stray bytes after its closing quote.
	ErrUnterminatedQuote = errors.New("csv: unterminated quoted field")
)

const (
	defaultDelimiter      byte = ','
	defaultMaxRecordBytes      = 16 << 20
)

// Option configures a CSVScanner.
type Option func(*CSVScanner)

// WithDelimiter sets the field delimiter. Invalid delimiters (NUL, the quote
// byte, CR and LF) are ignored and the current delimiter is kept.
func WithDelimiter(d byte) Option {
	return func(s *CSVScanner) {
		switch d {
		case 0, '"', '\n', '\r':
			return
		}
		s.delimiter = d
	}
}

// WithHeader skips the first non-blank record, which keeps the header row out
// of the decoded stream. A quoted multiline header is skipped as one record.
func WithHeader() Option {
	return func(s *CSVScanner) {
		s.skipHeader = true
	}
}

// WithMaxRecordBytes caps the bytes held for a single record, including its
// trailing newline. A value <= 0 disables the cap.
func WithMaxRecordBytes(n int) Option {
	return func(s *CSVScanner) {
		s.maxRecordBytes = n
	}
}

// CSVScanner streams delimited records from an io.Reader without buffering the
// whole input. Cells alias an internal buffer and stay valid only until the
// next call to Next.
type CSVScanner struct {
	reader         *bufio.Reader
	lineBuf        []byte
	byteCells      [][]byte
	delimiter      byte
	skipHeader     bool
	headerSkipped  bool
	maxRecordBytes int
	firstRecord    bool
	err            error
}

// NewScanner creates a streaming CSV scanner over r.
func NewScanner(r io.Reader, opts ...Option) *CSVScanner {
	s := &CSVScanner{
		reader:         bufio.NewReaderSize(r, 64*1024),
		lineBuf:        make([]byte, 0, 1024),
		byteCells:      make([][]byte, 0, 16),
		delimiter:      defaultDelimiter,
		maxRecordBytes: defaultMaxRecordBytes,
		firstRecord:    true,
	}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(s)
	}
	return s
}

// Next advances to the next record and reports whether one was assembled.
func (s *CSVScanner) Next() bool {
	if s.err != nil {
		return false
	}
	for {
		if !s.readRecord() {
			return false
		}

		record := s.lineBuf
		if s.firstRecord {
			s.firstRecord = false
			if hasUTF8BOM(record) {
				record = record[len(utf8BOM):]
			}
		}
		record = trimRecordEnd(record)
		if len(record) == 0 {
			continue
		}
		if s.skipHeader && !s.headerSkipped {
			s.headerSkipped = true
			continue
		}

		cells, ok := splitCSVLineInPlace(record, s.delimiter, s.byteCells[:0])
		if !ok {
			s.err = stencil.MarkMalformedInput(ErrUnterminatedQuote)
			return false
		}
		s.byteCells = cells
		return true
	}
}

// ScanFields visits the cells of the current record.
func (s *CSVScanner) ScanFields(yield stencil.FieldVisitor) error {
	for col, val := range s.byteCells {
		if !yield(col, nil, val) {
			break
		}
	}
	return nil
}

// Err reports the parse failure that stopped the scanner, if any.
func (s *CSVScanner) Err() error {
	return s.err
}

// readRecord accumulates physical lines until a newline outside quotes closes
// the record. It leaves the assembled bytes in lineBuf.
func (s *CSVScanner) readRecord() bool {
	s.lineBuf = s.lineBuf[:0]
	state := stateFieldStart

	for {
		chunk, readErr := s.reader.ReadSlice('\n')
		if s.maxRecordBytes > 0 && len(s.lineBuf)+len(chunk) > s.maxRecordBytes {
			s.err = stencil.MarkMalformedInput(ErrRecordTooLarge)
			return false
		}
		s.lineBuf = append(s.lineBuf, chunk...)

		var done bool
		state, done = advanceRecordState(state, s.delimiter, chunk)
		if state == stateMalformed {
			s.err = stencil.MarkMalformedInput(ErrUnterminatedQuote)
			return false
		}
		if done {
			return true
		}
		if readErr == nil || readErr == bufio.ErrBufferFull {
			continue
		}
		if readErr == io.EOF {
			if len(s.lineBuf) == 0 {
				return false
			}
			if state == stateQuoted {
				s.err = stencil.MarkMalformedInput(ErrUnterminatedQuote)
				return false
			}
			return true
		}
		s.err = readErr
		return false
	}
}

type recordState uint8

const (
	stateFieldStart recordState = iota
	stateUnquoted
	stateQuoted
	stateQuoteEnded
	stateMalformed
)

// advanceRecordState consumes one chunk of a record incrementally, carrying the
// quote state across chunks. It reports whether the chunk closed the record.
func advanceRecordState(state recordState, delimiter byte, chunk []byte) (recordState, bool) {
	for _, b := range chunk {
		switch state {
		case stateFieldStart:
			switch {
			case b == '"':
				state = stateQuoted
			case b == delimiter:
				// Empty field; the next byte starts another field.
			case b == '\n':
				return state, true
			default:
				state = stateUnquoted
			}
		case stateUnquoted:
			switch {
			case b == delimiter:
				state = stateFieldStart
			case b == '\n':
				return state, true
			}
		case stateQuoted:
			if b == '"' {
				state = stateQuoteEnded
			}
		case stateQuoteEnded:
			switch {
			case b == '"':
				state = stateQuoted
			case b == delimiter:
				state = stateFieldStart
			case b == '\n':
				return state, true
			case b == '\r':
				// Tolerate CRLF after a closing quote.
			default:
				return stateMalformed, false
			}
		default:
			return stateMalformed, false
		}
	}
	return state, false
}

var utf8BOM = [3]byte{0xEF, 0xBB, 0xBF}

func hasUTF8BOM(b []byte) bool {
	return len(b) >= len(utf8BOM) &&
		b[0] == utf8BOM[0] &&
		b[1] == utf8BOM[1] &&
		b[2] == utf8BOM[2]
}

func trimRecordEnd(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func splitCSVLineInPlace(record []byte, delimiter byte, dst [][]byte) ([][]byte, bool) {
	i, n := 0, len(record)
	for i <= n {
		if i == n {
			dst = append(dst, record[n:n])
			break
		}
		if record[i] == '"' {
			i++
			start := i
			writeIdx := i
			closed := false
			for i < n {
				if record[i] == '"' {
					if i+1 < n && record[i+1] == '"' {
						record[writeIdx] = '"'
						writeIdx++
						i += 2
						continue
					}
					closed = true
					i++
					break
				}
				record[writeIdx] = record[i]
				writeIdx++
				i++
			}
			if !closed || (i < n && record[i] != delimiter) {
				return dst, false
			}
			dst = append(dst, record[start:writeIdx])
			if i == n {
				break
			}
			i++
		} else {
			start := i
			for i < n && record[i] != delimiter {
				i++
			}
			dst = append(dst, record[start:i])
			if i == n {
				break
			}
			i++
		}
	}
	return dst, true
}
