package sink

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"time"
	"unsafe"

	"github.com/xdvi/stencil"
)

// AppendISO8601UTC formats t in UTC as YYYY-MM-DDTHH:MM:SSZ directly into dst with zero allocations.
func AppendISO8601UTC(dst []byte, t time.Time) []byte {
	u := t.UTC()
	year, month, day := u.Date()
	hour, min, sec := u.Clock()

	// Need 20 bytes: 4 (year) + 1 (-) + 2 (month) + 1 (-) + 2 (day) + 1 (T) + 2 (hour) + 1 (:) + 2 (min) + 1 (:) + 2 (sec) + 1 (Z)
	n := len(dst)
	dst = append(dst, "0000-00-00T00:00:00Z"...)
	b := dst[n:]

	// Year
	b[0] = byte('0' + (year/1000)%10)
	b[1] = byte('0' + (year/100)%10)
	b[2] = byte('0' + (year/10)%10)
	b[3] = byte('0' + year%10)

	// Month
	m := int(month)
	b[5] = byte('0' + m/10)
	b[6] = byte('0' + m%10)

	// Day
	b[8] = byte('0' + day/10)
	b[9] = byte('0' + day%10)

	// Hour
	b[11] = byte('0' + hour/10)
	b[12] = byte('0' + hour%10)

	// Minute
	b[14] = byte('0' + min/10)
	b[15] = byte('0' + min%10)

	// Second
	b[17] = byte('0' + sec/10)
	b[18] = byte('0' + sec%10)

	return dst
}

// AppendISO4217Cents formats an int64 cents value into standard decimal form (e.g. 1250 -> "12.50")
// directly into dst with zero heap allocations and no floating-point arithmetic.
func AppendISO4217Cents(dst []byte, cents int64) []byte {
	if cents == 0 {
		return append(dst, "0.00"...)
	}

	neg := cents < 0
	var ucents uint64
	if neg {
		dst = append(dst, '-')
		ucents = uint64(-cents)
	} else {
		ucents = uint64(cents)
	}

	whole := ucents / 100
	fraction := ucents % 100

	// Format whole part
	if whole == 0 {
		dst = append(dst, '0')
	} else {
		var wholeBuf [20]byte
		wIdx := len(wholeBuf)
		for whole > 0 {
			wIdx--
			wholeBuf[wIdx] = byte('0' + whole%10)
			whole /= 10
		}
		dst = append(dst, wholeBuf[wIdx:]...)
	}

	// Format decimal and two decimal places
	dst = append(dst, '.', byte('0'+fraction/10), byte('0'+fraction%10))
	return dst
}

// CSVEmitter serializes fields into CSV rows according to RFC 4180.
type CSVEmitter struct {
	w       *bufio.Writer
	fieldIdx int
	delim   byte
}

// NewCSVEmitter creates a new CSVEmitter writing to w.
func NewCSVEmitter(w io.Writer) *CSVEmitter {
	return &CSVEmitter{
		w:     bufio.NewWriter(w),
		delim: ',',
	}
}

// EmitField writes a column field, quoting if it contains delimiters, quotes, or newlines.
func (e *CSVEmitter) EmitField(col int, key []byte, val []byte) error {
	if e.fieldIdx > 0 {
		if err := e.w.WriteByte(e.delim); err != nil {
			return err
		}
	}
	e.fieldIdx++

	needsQuotes := false
	if bytes.IndexByte(val, e.delim) >= 0 || bytes.IndexByte(val, '"') >= 0 ||
		bytes.IndexByte(val, '\n') >= 0 || bytes.IndexByte(val, '\r') >= 0 {
		needsQuotes = true
	}

	if !needsQuotes {
		_, err := e.w.Write(val)
		return err
	}

	if err := e.w.WriteByte('"'); err != nil {
		return err
	}
	for i := 0; i < len(val); i++ {
		c := val[i]
		if c == '"' {
			if _, err := e.w.WriteString(`""`); err != nil {
				return err
			}
		} else {
			if err := e.w.WriteByte(c); err != nil {
				return err
			}
		}
	}
	return e.w.WriteByte('"')
}

// EndRow terminates the current row with a newline.
func (e *CSVEmitter) EndRow() error {
	e.fieldIdx = 0
	return e.w.WriteByte('\n')
}

// Flush flushes buffered bytes to the underlying writer.
func (e *CSVEmitter) Flush() error {
	return e.w.Flush()
}

// NDJSONEmitter serializes rows as newline-delimited JSON objects.
type NDJSONEmitter struct {
	w        *bufio.Writer
	fieldIdx int
}

// NewNDJSONEmitter creates a new NDJSONEmitter.
func NewNDJSONEmitter(w io.Writer) *NDJSONEmitter {
	return &NDJSONEmitter{w: bufio.NewWriter(w)}
}

// EmitField writes a JSON key-value pair.
func (e *NDJSONEmitter) EmitField(col int, key []byte, val []byte) error {
	if len(key) == 0 {
		return nil // Ignore keyless columns in JSON
	}

	if e.fieldIdx == 0 {
		if err := e.w.WriteByte('{'); err != nil {
			return err
		}
	} else {
		if err := e.w.WriteByte(','); err != nil {
			return err
		}
	}
	e.fieldIdx++

	// Write key: "<key>":
	if err := e.w.WriteByte('"'); err != nil {
		return err
	}
	if _, err := e.w.Write(key); err != nil {
		return err
	}
	if _, err := e.w.WriteString(`":`); err != nil {
		return err
	}

	// Determine if val is numeric or already JSON-formatted
	if isNumericOrBoolean(val) {
		_, err := e.w.Write(val)
		return err
	}

	// Otherwise quote as JSON string with escapes
	if err := e.w.WriteByte('"'); err != nil {
		return err
	}
	for i := 0; i < len(val); i++ {
		c := val[i]
		switch c {
		case '"':
			if _, err := e.w.WriteString(`\"`); err != nil {
				return err
			}
		case '\\':
			if _, err := e.w.WriteString(`\\`); err != nil {
				return err
			}
		case '\n':
			if _, err := e.w.WriteString(`\n`); err != nil {
				return err
			}
		case '\r':
			if _, err := e.w.WriteString(`\r`); err != nil {
				return err
			}
		case '\t':
			if _, err := e.w.WriteString(`\t`); err != nil {
				return err
			}
		default:
			if err := e.w.WriteByte(c); err != nil {
				return err
			}
		}
	}
	return e.w.WriteByte('"')
}

func isNumericOrBoolean(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	if bytes.Equal(b, []byte("true")) || bytes.Equal(b, []byte("false")) || bytes.Equal(b, []byte("null")) {
		return true
	}
	// Check if simple integer or decimal
	start := 0
	if b[0] == '-' || b[0] == '+' {
		start = 1
	}
	if start >= len(b) {
		return false
	}
	hasDigits := false
	hasDot := false
	for i := start; i < len(b); i++ {
		c := b[i]
		if c >= '0' && c <= '9' {
			hasDigits = true
		} else if c == '.' && !hasDot {
			hasDot = true
		} else {
			return false
		}
	}
	return hasDigits
}

// EndRow closes the JSON object and writes a newline.
func (e *NDJSONEmitter) EndRow() error {
	if e.fieldIdx == 0 {
		if _, err := e.w.WriteString("{}\n"); err != nil {
			return err
		}
	} else {
		if _, err := e.w.WriteString("}\n"); err != nil {
			return err
		}
	}
	e.fieldIdx = 0
	return nil
}

// Flush flushes buffered bytes.
func (e *NDJSONEmitter) Flush() error {
	return e.w.Flush()
}

// StreamEncoder symmetrically serializes typed structs into rows using pre-compiled schema bindings.
type StreamEncoder[T any] struct {
	schema   *stencil.Schema[T]
	emitter  stencil.RowEmitter
	fieldBuf []byte
}

// NewStreamEncoder creates a new StreamEncoder for struct T.
func NewStreamEncoder[T any](s *stencil.Schema[T], emitter stencil.RowEmitter) (*StreamEncoder[T], error) {
	if s == nil {
		return nil, errors.New("sink: schema cannot be nil")
	}
	if emitter == nil {
		return nil, errors.New("sink: emitter cannot be nil")
	}
	return &StreamEncoder[T]{
		schema:   s,
		emitter:  emitter,
		fieldBuf: make([]byte, 0, 256),
	}, nil
}

// Encode writes a single struct item to the emitter.
func (e *StreamEncoder[T]) Encode(item *T) error {
	if item == nil {
		return errors.New("sink: item cannot be nil")
	}

	base := unsafe.Pointer(item)
	bindings := e.schema.AllBindings()

	for _, b := range bindings {
		if b.Extract == nil {
			continue
		}
		e.fieldBuf = b.Extract(base, e.fieldBuf[:0])
		if err := e.emitter.EmitField(b.ColIndex, b.KeyBytes, e.fieldBuf); err != nil {
			return err
		}
	}

	return e.emitter.EndRow()
}

// EncodeAll serializes all items and flushes the output.
func (e *StreamEncoder[T]) EncodeAll(items []T) error {
	for i := range items {
		if err := e.Encode(&items[i]); err != nil {
			return err
		}
	}
	return e.Flush()
}

// Flush flushes the underlying emitter.
func (e *StreamEncoder[T]) Flush() error {
	return e.emitter.Flush()
}
