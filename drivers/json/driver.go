package json

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/xdvi/stencil"
)

// JSONScanner tokenizes streaming JSON array objects without loading the entire payload into RAM.
type JSONScanner struct {
	dec     *json.Decoder
	started bool
	done    bool
	err     error
}

// NewScanner creates a new streaming JSON scanner.
func NewScanner(r io.Reader) *JSONScanner {
	return &JSONScanner{dec: json.NewDecoder(r)}
}

// Next advances to the next object in the stream.
func (s *JSONScanner) Next() bool {
	if s.err != nil || s.done {
		return false
	}

	if !s.started {
		tok, err := s.dec.Token()
		if err != nil {
			if err != io.EOF {
				s.err = parseFailure(err)
			}
			s.done = true
			return false
		}
		if delim, ok := tok.(json.Delim); !ok || delim != '[' {
			s.err = stencil.MarkMalformedInput(fmt.Errorf("json: expected '[' at start of stream, got %s", describeToken(tok)))
			s.done = true
			return false
		}
		s.started = true
	}

	if !s.dec.More() {
		s.done = true
		if _, err := s.dec.Token(); err != nil && err != io.EOF { // consume closing ']'
			s.err = parseFailure(err)
		}
		return false
	}
	return true
}

// ScanFields decodes key-value pairs of the current JSON object.
func (s *JSONScanner) ScanFields(yield stencil.FieldVisitor) error {
	if s.err != nil {
		return s.err
	}
	if !s.started || s.done {
		return fmt.Errorf("json: ScanFields called before Next or after the stream ended")
	}

	tok, err := s.dec.Token()
	if err != nil {
		s.err = parseFailure(err)
		return s.err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		err = stencil.MarkMalformedInput(fmt.Errorf("expected '{' at start of object, got %v", tok))
		s.err = err
		return err
	}

	stopped := false
	for s.dec.More() {
		keyTok, err := s.dec.Token()
		if err != nil {
			s.err = parseFailure(err)
			return s.err
		}
		key, ok := keyTok.(string)
		if !ok {
			err = stencil.MarkMalformedInput(fmt.Errorf("expected string key, got %v", keyTok))
			s.err = err
			return err
		}

		var raw json.RawMessage
		if err := s.dec.Decode(&raw); err != nil {
			s.err = parseFailure(err)
			return s.err
		}

		// Drain the rest of the object without yielding so the decoder stays
		// positioned at the next row after a field-level failure.
		if stopped {
			continue
		}

		val, err := decodeValue(key, raw)
		if err != nil {
			s.err = err
			return err
		}

		if !yield(-1, []byte(key), val) {
			stopped = true
		}
	}

	_, err = s.dec.Token() // consume closing '}'
	if err != nil {
		s.err = parseFailure(err)
		return s.err
	}
	return nil
}

// Err returns the non-EOF error that stopped the scanner, if any.
func (s *JSONScanner) Err() error {
	return s.err
}

// decodeValue maps a raw JSON value to the flat byte representation the schema expects.
func decodeValue(key string, raw json.RawMessage) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}

	switch raw[0] {
	case '{', '[':
		return nil, fmt.Errorf("json: nested object or array value for key %q is not supported", key)
	case '"':
		var str string
		if err := json.Unmarshal(raw, &str); err != nil {
			return nil, err
		}
		return []byte(str), nil
	}

	if bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	return raw, nil
}

func describeToken(tok json.Token) string {
	if tok == nil {
		return "null"
	}
	if str, ok := tok.(string); ok {
		return strconv.Quote(str)
	}
	return fmt.Sprint(tok)
}

// parseFailure tags JSON syntax and truncation errors as caller-caused input.
// Read-side failures from the underlying reader stay unmarked.
func parseFailure(err error) error {
	if err == nil {
		return nil
	}
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) || errors.Is(err, io.ErrUnexpectedEOF) {
		return stencil.MarkMalformedInput(err)
	}
	return err
}
