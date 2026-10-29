package json

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/xdvi/stencil"
)

// DefaultBufferSize is the sliding window buffer size (64 KB).
const DefaultBufferSize = 64 * 1024

type dfaState int

const (
	stateInit dfaState = iota
	stateArrayStarted
	stateInObject
	stateObjectEnded
	stateArrayEnded
	stateEOF
	stateError
)

// JSONScanner is a high-performance, zero-allocation DFA streaming scanner
// for JSON arrays of objects.
type JSONScanner struct {
	r         io.Reader
	buf       []byte
	rpos      int
	wpos      int
	state     dfaState
	err       error
	readerErr error
	eof       bool

	// Token scratch buffers to avoid heap allocations
	keyScratch []byte
	valScratch []byte
}

// NewScanner creates a new streaming JSON scanner with a 64 KB sliding window.
func NewScanner(r io.Reader) *JSONScanner {
	return &JSONScanner{
		r:          r,
		buf:        make([]byte, DefaultBufferSize),
		state:      stateInit,
		keyScratch: make([]byte, 0, 256),
		valScratch: make([]byte, 0, 1024),
	}
}

// Next advances to the next object in the JSON array.
func (s *JSONScanner) Next() bool {
	if s.err != nil || s.state == stateArrayEnded || s.state == stateEOF || s.state == stateError {
		return false
	}

	for {
		s.skipWhitespace()

		switch s.state {
		case stateInit:
			b, err := s.peekByte()
			if err != nil {
				if err == io.EOF {
					s.state = stateEOF
					return false
				}
				s.err = err
				s.state = stateError
				return false
			}

			if b != '[' {
				tok := s.describeNonArrayToken()
				s.err = stencil.MarkMalformedInput(fmt.Errorf("json: expected '[' at start of stream, got %s", tok))
				s.state = stateError
				return false
			}

			s.rpos++ // consume '['
			s.state = stateArrayStarted

		case stateArrayStarted, stateObjectEnded:
			s.skipWhitespace()
			b, err := s.peekByte()
			if err != nil {
				if err == io.EOF {
					s.err = stencil.MarkMalformedInput(io.ErrUnexpectedEOF)
					s.state = stateError
					return false
				}
				s.err = err
				s.state = stateError
				return false
			}

			if b == ']' {
				s.rpos++ // consume ']'
				s.state = stateArrayEnded
				return false
			}

			if s.state == stateObjectEnded {
				if b == ',' {
					s.rpos++ // consume ','
					s.skipWhitespace()
					b, err = s.peekByte()
					if err != nil {
						if err == io.EOF {
							s.err = stencil.MarkMalformedInput(io.ErrUnexpectedEOF)
						} else {
							s.err = err
						}
						s.state = stateError
						return false
					}
				} else {
					s.err = stencil.MarkMalformedInput(fmt.Errorf("json: expected ',' or ']', got %q", b))
					s.state = stateError
					return false
				}
			}

			if b != '{' {
				s.err = stencil.MarkMalformedInput(fmt.Errorf("json: expected '{' at start of object, got %q", b))
				s.state = stateError
				return false
			}

			s.rpos++ // consume '{'
			s.state = stateInObject
			return true

		default:
			return false
		}
	}
}

// ScanFields decodes key-value pairs of the current JSON object.
func (s *JSONScanner) ScanFields(yield stencil.FieldVisitor) error {
	if s.err != nil {
		return s.err
	}
	if s.state != stateInObject {
		return fmt.Errorf("json: ScanFields called before Next or after the stream ended")
	}

	stopped := false

	for {
		s.skipWhitespace()
		b, err := s.peekByte()
		if err != nil {
			if err == io.EOF {
				s.err = stencil.MarkMalformedInput(io.ErrUnexpectedEOF)
			} else {
				s.err = err
			}
			s.state = stateError
			return s.err
		}

		if b == '}' {
			s.rpos++ // consume '}'
			s.state = stateObjectEnded
			return nil
		}

		if b == ',' {
			s.rpos++ // consume ','
			s.skipWhitespace()
		}

		// Read key
		key, err := s.readString(&s.keyScratch)
		if err != nil {
			s.err = stencil.MarkMalformedInput(err)
			s.state = stateError
			return s.err
		}

		s.skipWhitespace()
		colon, err := s.readByte()
		if err != nil {
			if err == io.EOF {
				s.err = stencil.MarkMalformedInput(io.ErrUnexpectedEOF)
			} else {
				s.err = err
			}
			s.state = stateError
			return s.err
		}
		if colon != ':' {
			s.err = stencil.MarkMalformedInput(fmt.Errorf("json: expected ':', got %q", colon))
			s.state = stateError
			return s.err
		}

		s.skipWhitespace()

		// Read value
		val, err := s.readValue(key)
		if err != nil {
			s.err = err
			s.state = stateError
			return s.err
		}

		if !stopped {
			if !yield(-1, key, val) {
				stopped = true
			}
		}
	}
}

// Err returns the non-EOF error that stopped the scanner, if any.
func (s *JSONScanner) Err() error {
	return s.err
}

func (s *JSONScanner) readValue(key []byte) ([]byte, error) {
	b, err := s.peekByte()
	if err != nil {
		if err == io.EOF {
			return nil, stencil.MarkMalformedInput(io.ErrUnexpectedEOF)
		}
		return nil, err
	}

	switch b {
	case '{', '[':
		return nil, stencil.MarkMalformedInput(fmt.Errorf("json: nested object or array value for key %q is not supported", key))

	case '"':
		return s.readString(&s.valScratch)

	case 'n': // null
		word, err := s.readWord()
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(word, []byte("null")) {
			return nil, stencil.MarkMalformedInput(fmt.Errorf("json: invalid literal %q", word))
		}
		return nil, nil // null returns nil slice

	case 't', 'f': // true or false
		word, err := s.readWord()
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(word, []byte("true")) && !bytes.Equal(word, []byte("false")) {
			return nil, stencil.MarkMalformedInput(fmt.Errorf("json: invalid literal %q", word))
		}
		return word, nil

	default:
		// Number
		if (b >= '0' && b <= '9') || b == '-' || b == '+' {
			return s.readNumber()
		}
		return nil, stencil.MarkMalformedInput(fmt.Errorf("json: invalid character %q looking for beginning of value", b))
	}
}

func (s *JSONScanner) readString(scratch *[]byte) ([]byte, error) {
	quote, err := s.readByte()
	if err != nil {
		return nil, err
	}
	if quote != '"' {
		return nil, fmt.Errorf("expected string beginning with '\"', got %q", quote)
	}

	start := s.rpos
	hasEscapes := false

	for {
		if s.rpos >= s.wpos {
			if err := s.fill(); err != nil {
				if err == io.EOF {
					return nil, io.ErrUnexpectedEOF
				}
				return nil, err
			}
		}

		c := s.buf[s.rpos]
		if c == '"' {
			// Found closing quote without unescaping needed
			if !hasEscapes {
				res := s.buf[start:s.rpos]
				s.rpos++
				return res, nil
			}
			break
		}
		if c == '\\' {
			hasEscapes = true
			s.rpos += 2 // skip escape and char
			continue
		}
		s.rpos++
	}

	// We have escapes, parse from start to s.rpos
	raw := s.buf[start:s.rpos]
	s.rpos++ // consume closing '"'

	*scratch = (*scratch)[:0]
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c != '\\' {
			*scratch = append(*scratch, c)
			continue
		}

		i++
		if i >= len(raw) {
			return nil, io.ErrUnexpectedEOF
		}
		esc := raw[i]
		switch esc {
		case '"':
			*scratch = append(*scratch, '"')
		case '\\':
			*scratch = append(*scratch, '\\')
		case '/':
			*scratch = append(*scratch, '/')
		case 'b':
			*scratch = append(*scratch, '\b')
		case 'f':
			*scratch = append(*scratch, '\f')
		case 'n':
			*scratch = append(*scratch, '\n')
		case 'r':
			*scratch = append(*scratch, '\r')
		case 't':
			*scratch = append(*scratch, '\t')
		case 'u':
			if i+4 >= len(raw) {
				return nil, errors.New("incomplete unicode escape")
			}
			r, err := decodeHex4(raw[i+1 : i+5])
			if err != nil {
				return nil, err
			}
			i += 4

			// Handle UTF-16 surrogate pairs
			if utf16.IsSurrogate(r) {
				if i+6 < len(raw) && raw[i+1] == '\\' && raw[i+2] == 'u' {
					r2, err2 := decodeHex4(raw[i+3 : i+7])
					if err2 == nil {
						combined := utf16.DecodeRune(r, r2)
						if combined != utf8.RuneError {
							var utf8Buf [4]byte
							n := utf8.EncodeRune(utf8Buf[:], combined)
							*scratch = append(*scratch, utf8Buf[:n]...)
							i += 6
							continue
						}
					}
				}
			}

			var utf8Buf [4]byte
			n := utf8.EncodeRune(utf8Buf[:], r)
			*scratch = append(*scratch, utf8Buf[:n]...)

		default:
			return nil, fmt.Errorf("invalid escape character %q", esc)
		}
	}

	return *scratch, nil
}

func decodeHex4(b []byte) (rune, error) {
	var val rune
	for _, c := range b {
		val <<= 4
		switch {
		case c >= '0' && c <= '9':
			val |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			val |= rune(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			val |= rune(c - 'A' + 10)
		default:
			return 0, fmt.Errorf("invalid hex digit %q", c)
		}
	}
	return val, nil
}

func (s *JSONScanner) readWord() ([]byte, error) {
	start := s.rpos
	for {
		if s.rpos >= s.wpos {
			if err := s.fill(); err != nil && err != io.EOF {
				return nil, err
			}
			if s.rpos >= s.wpos {
				break
			}
		}
		c := s.buf[s.rpos]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			s.rpos++
		} else {
			break
		}
	}
	return s.buf[start:s.rpos], nil
}

func (s *JSONScanner) readNumber() ([]byte, error) {
	start := s.rpos
	for {
		if s.rpos >= s.wpos {
			if err := s.fill(); err != nil && err != io.EOF {
				return nil, err
			}
			if s.rpos >= s.wpos {
				break
			}
		}
		c := s.buf[s.rpos]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E' {
			s.rpos++
		} else {
			break
		}
	}
	return s.buf[start:s.rpos], nil
}

func (s *JSONScanner) skipWhitespace() {
	for {
		for s.rpos < s.wpos {
			c := s.buf[s.rpos]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				s.rpos++
			} else {
				return
			}
		}
		if err := s.fill(); err != nil {
			return
		}
	}
}

func (s *JSONScanner) peekByte() (byte, error) {
	if s.rpos >= s.wpos {
		if err := s.fill(); err != nil {
			return 0, err
		}
	}
	return s.buf[s.rpos], nil
}

func (s *JSONScanner) readByte() (byte, error) {
	if s.rpos >= s.wpos {
		if err := s.fill(); err != nil {
			return 0, err
		}
	}
	b := s.buf[s.rpos]
	s.rpos++
	return b, nil
}

func (s *JSONScanner) fill() error {
	if s.eof {
		return io.EOF
	}

	// Slide unconsumed bytes to buffer front
	if s.rpos > 0 {
		n := copy(s.buf, s.buf[s.rpos:s.wpos])
		s.wpos = n
		s.rpos = 0
	}

	// Grow buffer if completely full
	if s.wpos == len(s.buf) {
		newBuf := make([]byte, len(s.buf)*2)
		copy(newBuf, s.buf)
		s.buf = newBuf
	}

	n, err := s.r.Read(s.buf[s.wpos:])
	s.wpos += n
	if err != nil {
		if err == io.EOF {
			s.eof = true
			if s.rpos >= s.wpos {
				return io.EOF
			}
			return nil
		}
		s.readerErr = err
		return err
	}
	return nil
}

func (s *JSONScanner) describeNonArrayToken() string {
	s.skipWhitespace()
	if s.rpos >= s.wpos {
		return "EOF"
	}
	c := s.buf[s.rpos]
	if c == '{' {
		return "{"
	}
	if c == '"' {
		s.rpos++
		start := s.rpos
		for s.rpos < s.wpos && s.buf[s.rpos] != '"' {
			s.rpos++
		}
		res := string(s.buf[start:s.rpos])
		if s.rpos < s.wpos && s.buf[s.rpos] == '"' {
			s.rpos++
		}
		return res
	}

	start := s.rpos
	for s.rpos < s.wpos {
		b := s.buf[s.rpos]
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == ',' || b == ']' || b == '}' {
			break
		}
		s.rpos++
	}
	return string(s.buf[start:s.rpos])
}
