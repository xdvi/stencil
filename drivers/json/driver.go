package json

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/xdvi/stencil"
)

// JSONScanner tokenizes streaming JSON array objects without loading the entire payload into RAM.
type JSONScanner struct {
	dec     *json.Decoder
	inArray bool
	err     error
}

// NewScanner creates a new streaming JSON scanner.
func NewScanner(r io.Reader) *JSONScanner {
	return &JSONScanner{dec: json.NewDecoder(r)}
}

// Next advances to the next object in the stream.
func (s *JSONScanner) Next() bool {
	if !s.inArray {
		tok, err := s.dec.Token()
		if err != nil {
			return false
		}
		if delim, ok := tok.(json.Delim); ok && delim == '[' {
			s.inArray = true
		}
	}

	if s.inArray && !s.dec.More() {
		_, _ = s.dec.Token() // consume closing ']'
		return false
	}
	return s.dec.More()
}

// ScanFields decodes key-value pairs of the current JSON object.
func (s *JSONScanner) ScanFields(yield stencil.FieldVisitor) error {
	tok, err := s.dec.Token()
	if err != nil {
		return err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("expected '{' at start of object, got %v", tok)
	}

	for s.dec.More() {
		keyTok, err := s.dec.Token()
		if err != nil {
			return err
		}
		key, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("expected string key, got %v", keyTok)
		}

		var raw json.RawMessage
		if err := s.dec.Decode(&raw); err != nil {
			return err
		}

		cleanVal := cleanJSONRaw(raw)
		if !yield(-1, []byte(key), cleanVal) {
			break
		}
	}

	_, err = s.dec.Token() // consume closing '}'
	return err
}

// Err returns the first non-EOF error encountered.
func (s *JSONScanner) Err() error {
	return s.err
}

func cleanJSONRaw(b []byte) []byte {
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		return b[1 : len(b)-1]
	}
	return b
}
