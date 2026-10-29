package schema

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"github.com/xdvi/stencil"
	"github.com/xdvi/stencil/scalars"
)

// FieldOption configures a field binding in SchemaBuilder.
type FieldOption func(*fieldOptions)

type fieldOptions struct {
	col      int
	key      string
	required bool
}

// WithCol assigns a zero-based column index to the field binding.
func WithCol(col int) FieldOption {
	return func(o *fieldOptions) {
		o.col = col
	}
}

// WithKey assigns a named key string to the field binding.
func WithKey(key string) FieldOption {
	return func(o *fieldOptions) {
		o.key = key
	}
}

// WithRequired marks the field as mandatory during row validation.
func WithRequired() FieldOption {
	return func(o *fieldOptions) {
		o.required = true
	}
}

func applyOptions(opts []FieldOption) fieldOptions {
	cfg := fieldOptions{col: -1}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

// SchemaBuilder constructs a compiled stencil.Schema[T] programmatically using
// pointer arithmetic to determine struct field offsets without any runtime reflection.
type SchemaBuilder[T any] struct {
	bindings []stencil.FieldBinding
	err      error
}

// NewBuilder creates a new fluent, reflection-free SchemaBuilder for struct T.
func NewBuilder[T any]() *SchemaBuilder[T] {
	return &SchemaBuilder[T]{}
}

func offsetOf[T, F any](selector func(*T) *F) uintptr {
	var dummy T
	base := uintptr(unsafe.Pointer(&dummy))
	target := uintptr(unsafe.Pointer(selector(&dummy)))
	return target - base
}

// FieldString registers a string field on T using pointer arithmetic to resolve its offset.
func (b *SchemaBuilder[T]) FieldString(name string, selector func(*T) *string, opts ...FieldOption) *SchemaBuilder[T] {
	if b.err != nil {
		return b
	}
	if name == "" {
		b.err = errors.New("schema: field name cannot be empty")
		return b
	}
	if selector == nil {
		b.err = fmt.Errorf("schema: field %s selector cannot be nil", name)
		return b
	}

	cfg := applyOptions(opts)
	if cfg.col < 0 && cfg.key == "" {
		b.err = fmt.Errorf("schema: field %s must specify WithCol or WithKey", name)
		return b
	}

	offset := offsetOf(selector)

	injector := func(base unsafe.Pointer, row int, val []byte, errOut *stencil.RowError) bool {
		ptr := (*string)(unsafe.Add(base, offset))
		*ptr = unsafe.String(unsafe.SliceData(val), len(val))
		return true
	}

	extractor := func(base unsafe.Pointer, dst []byte) []byte {
		val := *(*string)(unsafe.Add(base, offset))
		return append(dst, val...)
	}

	isZero := func(base unsafe.Pointer) bool {
		return *(*string)(unsafe.Add(base, offset)) == ""
	}

	detach := func(base unsafe.Pointer) {
		ptr := (*string)(unsafe.Add(base, offset))
		*ptr = strings.Clone(*ptr)
	}

	b.bindings = append(b.bindings, stencil.FieldBinding{
		ColIndex: cfg.col,
		KeyName:  cfg.key,
		KeyBytes: []byte(cfg.key),
		Required: cfg.required,
		Name:     name,
		Offset:   offset,
		Inject:   injector,
		Extract:  extractor,
		IsZero:   isZero,
		Detach:   detach,
	})

	return b
}

// FieldCurrency registers an int64 currency field (stored in cents) using pointer arithmetic.
func (b *SchemaBuilder[T]) FieldCurrency(name string, selector func(*T) *int64, opts ...FieldOption) *SchemaBuilder[T] {
	if b.err != nil {
		return b
	}
	if name == "" {
		b.err = errors.New("schema: field name cannot be empty")
		return b
	}
	if selector == nil {
		b.err = fmt.Errorf("schema: field %s selector cannot be nil", name)
		return b
	}

	cfg := applyOptions(opts)
	if cfg.col < 0 && cfg.key == "" {
		b.err = fmt.Errorf("schema: field %s must specify WithCol or WithKey", name)
		return b
	}

	offset := offsetOf(selector)

	injector := func(base unsafe.Pointer, row int, val []byte, errOut *stencil.RowError) bool {
		if len(val) == 0 {
			return true
		}
		cents, err := scalars.ParseCurrencyCents(val)
		if err != nil {
			*errOut = stencil.RowError{
				Row:      row,
				Col:      name,
				Code:     "invalid_currency",
				Expected: "ISO 4217 Currency",
				Actual:   string(val),
				Message:  err.Error(),
			}
			return false
		}
		*(*int64)(unsafe.Add(base, offset)) = cents
		return true
	}

	extractor := func(base unsafe.Pointer, dst []byte) []byte {
		cents := *(*int64)(unsafe.Add(base, offset))
		return scalars.AppendCurrencyCents(dst, cents)
	}

	isZero := func(base unsafe.Pointer) bool {
		return *(*int64)(unsafe.Add(base, offset)) == 0
	}

	detach := func(base unsafe.Pointer) {}

	b.bindings = append(b.bindings, stencil.FieldBinding{
		ColIndex: cfg.col,
		KeyName:  cfg.key,
		KeyBytes: []byte(cfg.key),
		Required: cfg.required,
		Name:     name,
		Offset:   offset,
		Inject:   injector,
		Extract:  extractor,
		IsZero:   isZero,
		Detach:   detach,
	})

	return b
}

// FieldISO8601 registers a time.Time timestamp field using pointer arithmetic.
func (b *SchemaBuilder[T]) FieldISO8601(name string, selector func(*T) *time.Time, opts ...FieldOption) *SchemaBuilder[T] {
	if b.err != nil {
		return b
	}
	if name == "" {
		b.err = errors.New("schema: field name cannot be empty")
		return b
	}
	if selector == nil {
		b.err = fmt.Errorf("schema: field %s selector cannot be nil", name)
		return b
	}

	cfg := applyOptions(opts)
	if cfg.col < 0 && cfg.key == "" {
		b.err = fmt.Errorf("schema: field %s must specify WithCol or WithKey", name)
		return b
	}

	offset := offsetOf(selector)

	injector := func(base unsafe.Pointer, row int, val []byte, errOut *stencil.RowError) bool {
		if len(val) == 0 {
			return true
		}
		ts, err := scalars.ParseISO8601(val)
		if err != nil {
			*errOut = stencil.RowError{
				Row:      row,
				Col:      name,
				Code:     "invalid_iso8601",
				Expected: "YYYY-MM-DDTHH:MM:SSZ",
				Actual:   string(val),
				Message:  err.Error(),
			}
			return false
		}
		*(*time.Time)(unsafe.Add(base, offset)) = ts
		return true
	}

	extractor := func(base unsafe.Pointer, dst []byte) []byte {
		ts := *(*time.Time)(unsafe.Add(base, offset))
		return scalars.AppendISO8601(dst, ts)
	}

	isZero := func(base unsafe.Pointer) bool {
		return (*(*time.Time)(unsafe.Add(base, offset))).IsZero()
	}

	detach := func(base unsafe.Pointer) {}

	b.bindings = append(b.bindings, stencil.FieldBinding{
		ColIndex: cfg.col,
		KeyName:  cfg.key,
		KeyBytes: []byte(cfg.key),
		Required: cfg.required,
		Name:     name,
		Offset:   offset,
		Inject:   injector,
		Extract:  extractor,
		IsZero:   isZero,
		Detach:   detach,
	})

	return b
}

// FieldCustom registers a generic custom field F using pointer arithmetic and custom converter functions.
func FieldCustom[T, F any](
	b *SchemaBuilder[T],
	name string,
	selector func(*T) *F,
	parse func([]byte) (F, error),
	format func(F, []byte) []byte,
	isZero func(F) bool,
	opts ...FieldOption,
) *SchemaBuilder[T] {
	if b.err != nil {
		return b
	}
	if name == "" {
		b.err = errors.New("schema: field name cannot be empty")
		return b
	}
	if selector == nil {
		b.err = fmt.Errorf("schema: field %s selector cannot be nil", name)
		return b
	}
	if parse == nil {
		b.err = fmt.Errorf("schema: field %s parse function cannot be nil", name)
		return b
	}

	cfg := applyOptions(opts)
	if cfg.col < 0 && cfg.key == "" {
		b.err = fmt.Errorf("schema: field %s must specify WithCol or WithKey", name)
		return b
	}

	offset := offsetOf(selector)

	injector := func(base unsafe.Pointer, row int, val []byte, errOut *stencil.RowError) bool {
		if len(val) == 0 {
			return true
		}
		v, err := parse(val)
		if err != nil {
			*errOut = stencil.RowError{
				Row:      row,
				Col:      name,
				Code:     "invalid_field",
				Expected: "Custom Format",
				Actual:   string(val),
				Message:  err.Error(),
			}
			return false
		}
		*(*F)(unsafe.Add(base, offset)) = v
		return true
	}

	var extractor stencil.FieldExtractor
	if format != nil {
		extractor = func(base unsafe.Pointer, dst []byte) []byte {
			v := *(*F)(unsafe.Add(base, offset))
			return format(v, dst)
		}
	}

	var zeroCheck func(base unsafe.Pointer) bool
	if isZero != nil {
		zeroCheck = func(base unsafe.Pointer) bool {
			v := *(*F)(unsafe.Add(base, offset))
			return isZero(v)
		}
	} else {
		zeroCheck = func(base unsafe.Pointer) bool { return false }
	}

	b.bindings = append(b.bindings, stencil.FieldBinding{
		ColIndex: cfg.col,
		KeyName:  cfg.key,
		KeyBytes: []byte(cfg.key),
		Required: cfg.required,
		Name:     name,
		Offset:   offset,
		Inject:   injector,
		Extract:  extractor,
		IsZero:   zeroCheck,
		Detach:   func(base unsafe.Pointer) {},
	})

	return b
}

// Build compiles the configured bindings into a stencil.Schema[T].
func (b *SchemaBuilder[T]) Build() (*stencil.Schema[T], error) {
	if b.err != nil {
		return nil, b.err
	}
	if len(b.bindings) == 0 {
		return nil, errors.New("schema: no fields defined in builder")
	}
	return stencil.NewSchemaFromBindings[T](b.bindings)
}
