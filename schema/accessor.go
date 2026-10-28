package schema

import (
	"unsafe"

	"github.com/xdvi/stencil"
)

type fieldInjector func(base unsafe.Pointer, row int, val []byte, errOut *stencil.RowError) bool
type fieldExtractor func(base unsafe.Pointer, dst []byte) []byte

type FieldBinding struct {
	ColIndex int
	KeyName  string
	KeyBytes []byte
	Required bool
	Offset   uintptr
	Inject   fieldInjector
	Extract  fieldExtractor
}
