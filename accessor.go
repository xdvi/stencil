package stencil

import (
	"unsafe"

	"github.com/xdvi/stencil/memory"
)

// FieldInjector parses raw bytes and injects the typed value into struct memory at base.
type FieldInjector func(base unsafe.Pointer, row int, val []byte, errOut *RowError) bool

// FieldExtractor formats the typed value at base and appends it to dst.
type FieldExtractor func(base unsafe.Pointer, dst []byte) []byte

// FieldBinding defines a compiled field mapping.
type FieldBinding struct {
	ColIndex    int
	KeyName     string
	KeyBytes    []byte
	Required    bool
	Name        string
	Offset      uintptr
	Inject      FieldInjector
	Extract     FieldExtractor
	IsZero      func(base unsafe.Pointer) bool
	Detach      func(base unsafe.Pointer)
	DetachArena func(base unsafe.Pointer, arena *memory.StringArena)
}
