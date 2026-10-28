package stencil

import (
	"unsafe"
)

type fieldInjector func(base unsafe.Pointer, row int, val []byte, errOut *RowError) bool
type fieldExtractor func(base unsafe.Pointer, dst []byte) []byte

type FieldBinding struct {
	ColIndex int
	KeyName  string
	KeyBytes []byte
	Required bool
	Name     string
	Offset   uintptr
	Inject   fieldInjector
	Extract  fieldExtractor
	IsZero   func(base unsafe.Pointer) bool
	Detach   func(base unsafe.Pointer)
}
