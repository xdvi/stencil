package csv

import (
	"bufio"
	"io"

	"github.com/xdvi/stencil"
)

type CSVScanner struct {
	reader    *bufio.Reader
	lineBuf   []byte
	byteCells [][]byte
	err       error
}

func NewScanner(r io.Reader) *CSVScanner {
	return &CSVScanner{
		reader:    bufio.NewReaderSize(r, 64*1024),
		lineBuf:   make([]byte, 0, 1024),
		byteCells: make([][]byte, 0, 16),
	}
}

func (s *CSVScanner) Next() bool {
	s.lineBuf = s.lineBuf[:0]
	for {
		line, err := s.reader.ReadSlice('\n')
		s.lineBuf = append(s.lineBuf, line...)
		if err == nil || err == io.EOF {
			if len(s.lineBuf) == 0 && err == io.EOF {
				return false
			}
			break
		}
		if err != bufio.ErrBufferFull {
			s.err = err
			return false
		}
	}

	var ok bool
	s.byteCells, ok = splitCSVLineInPlace(s.lineBuf, s.byteCells[:0])
	if !ok {
		s.err = io.ErrUnexpectedEOF
		return false
	}
	return true
}

func (s *CSVScanner) ScanFields(yield stencil.FieldVisitor) error {
	for col, val := range s.byteCells {
		if !yield(col, nil, val) {
			break
		}
	}
	return nil
}

func (s *CSVScanner) Err() error {
	return s.err
}

func splitCSVLineInPlace(line []byte, dst [][]byte) ([][]byte, bool) {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	i, n := 0, len(line)
	for i <= n {
		if i == n {
			dst = append(dst, line[n:n])
			break
		}
		if line[i] == '"' {
			i++
			start := i
			writeIdx := i
			closed := false
			for i < n {
				if line[i] == '"' {
					if i+1 < n && line[i+1] == '"' {
						line[writeIdx] = '"'
						writeIdx++
						i += 2
						continue
					}
					closed = true
					i++
					break
				}
				line[writeIdx] = line[i]
				writeIdx++
				i++
			}
			if !closed || (i < n && line[i] != ',') {
				return dst, false
			}
			dst = append(dst, line[start:writeIdx])
			if i == n {
				break
			}
			i++
		} else {
			start := i
			for i < n && line[i] != ',' {
				i++
			}
			dst = append(dst, line[start:i])
			if i == n {
				break
			}
			i++
		}
	}
	return dst, true
}
