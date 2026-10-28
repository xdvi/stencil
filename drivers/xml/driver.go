package xml

import (
	"encoding/xml"
	"io"

	"github.com/xdvi/stencil"
)

// XMLScanner streams repeated XML elements one by one without buffering the entire document into RAM.
type XMLScanner struct {
	dec     *xml.Decoder
	itemTag string
	err     error
}

// NewScanner creates a new streaming XML scanner for the given repeating element tag.
func NewScanner(r io.Reader, itemTag string) *XMLScanner {
	return &XMLScanner{
		dec:     xml.NewDecoder(r),
		itemTag: itemTag,
	}
}

// Next locates the start element matching itemTag.
func (s *XMLScanner) Next() bool {
	for {
		tok, err := s.dec.Token()
		if err != nil {
			if err != io.EOF {
				s.err = err
			}
			return false
		}
		if start, ok := tok.(xml.StartElement); ok && start.Name.Local == s.itemTag {
			return true
		}
	}
}

// ScanFields decodes child tags of the current element into key-value pairs.
func (s *XMLScanner) ScanFields(yield stencil.FieldVisitor) error {
	for {
		tok, err := s.dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			key := t.Name.Local
			var textVal string
			if err := s.dec.DecodeElement(&textVal, &t); err != nil {
				return err
			}
			if !yield(-1, []byte(key), []byte(textVal)) {
				return nil
			}
		case xml.EndElement:
			if t.Name.Local == s.itemTag {
				return nil
			}
		}
	}
}

// Err returns the first non-EOF error encountered.
func (s *XMLScanner) Err() error {
	return s.err
}
