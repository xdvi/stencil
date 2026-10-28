package stencil

// FieldVisitor receives a raw column index (or -1 if key-based), field key (or nil if index-based), and raw bytes.
type FieldVisitor func(col int, key []byte, val []byte) bool

// RowScanner is the low-level tokenizer primitive implemented by all format readers.
type RowScanner interface {
	Next() bool
	ScanFields(yield FieldVisitor) error
	Err() error
}

// RowEmitter is the low-level serialization primitive implemented by all format writers.
type RowEmitter interface {
	EmitField(col int, key []byte, val []byte) error
	EndRow() error
	Flush() error
}
