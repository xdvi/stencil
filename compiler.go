package stencil

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/xdvi/stencil/scalars"
)

// Schema represents a pre-compiled mapping plan for struct type T.
type Schema[T any] struct {
	colBindings []FieldBinding
	keyBindings []FieldBinding
	all         []FieldBinding
	zero        T
	required    []FieldBinding
}

// Compile inspects struct tags once at startup and compiles memory offsets and typed injectors.
func Compile[T any]() (*Schema[T], error) {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil || t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("stencil: schema requires a struct type")
	}

	var cols []FieldBinding
	var keys []FieldBinding
	var all []FieldBinding
	var reqs []FieldBinding
	colOwners := make(map[int]string, t.NumField())
	keyOwners := make(map[string]string, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("stencil")
		if tag == "" {
			continue
		}

		binding, err := parseFieldTag(field, tag)
		if err != nil {
			return nil, fmt.Errorf("stencil: field %s: %w", field.Name, err)
		}

		all = append(all, binding)

		if binding.ColIndex >= 0 {
			if owner, ok := colOwners[binding.ColIndex]; ok {
				return nil, fmt.Errorf("stencil: field %s: duplicate col:%d (already used by %s)", field.Name, binding.ColIndex, owner)
			}
			colOwners[binding.ColIndex] = field.Name
			for len(cols) <= binding.ColIndex {
				cols = append(cols, FieldBinding{ColIndex: -1})
			}
			cols[binding.ColIndex] = binding
		}
		if len(binding.KeyBytes) > 0 {
			if owner, ok := keyOwners[binding.KeyName]; ok {
				return nil, fmt.Errorf("stencil: field %s: duplicate key %q (already used by %s)", field.Name, binding.KeyName, owner)
			}
			keyOwners[binding.KeyName] = field.Name
			keys = append(keys, binding)
		}
		if binding.Required {
			reqs = append(reqs, binding)
		}
	}

	return &Schema[T]{
		colBindings: cols,
		keyBindings: keys,
		all:         all,
		zero:        zero,
		required:    reqs,
	}, nil
}

func parseFieldTag(field reflect.StructField, tag string) (FieldBinding, error) {
	if field.PkgPath != "" {
		return FieldBinding{}, fmt.Errorf("tag on unexported field")
	}

	parts := strings.Split(tag, ",")
	colIdx := -1
	var keyName string
	var required, isCurrency, isISO8601 bool

	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case p == "":
			return FieldBinding{}, fmt.Errorf("empty tag token")
		case strings.HasPrefix(p, "col:"):
			if colIdx >= 0 {
				return FieldBinding{}, fmt.Errorf("duplicate col:%d", colIdx)
			}
			idx, err := parseColIndex(strings.TrimPrefix(p, "col:"))
			if err != nil {
				return FieldBinding{}, err
			}
			colIdx = idx
		case strings.HasPrefix(p, "key:"):
			if keyName != "" {
				return FieldBinding{}, fmt.Errorf("duplicate key %q", keyName)
			}
			name := strings.TrimPrefix(p, "key:")
			if name == "" {
				return FieldBinding{}, fmt.Errorf("empty key name")
			}
			keyName = name
		case p == "required":
			required = true
		case p == "currency":
			isCurrency = true
		case p == "iso8601":
			isISO8601 = true
		default:
			return FieldBinding{}, fmt.Errorf("unknown tag token %q", p)
		}
	}

	if colIdx < 0 && keyName == "" {
		return FieldBinding{}, fmt.Errorf("tag must declare col or key")
	}

	offset := field.Offset
	name := field.Name
	var injector fieldInjector
	var extractor fieldExtractor
	var isZero func(base unsafe.Pointer) bool
	var detach func(base unsafe.Pointer)

	switch {
	case field.Type == reflect.TypeOf(""):
		if isCurrency {
			return FieldBinding{}, fmt.Errorf("currency requires an int64 field")
		}
		if isISO8601 {
			return FieldBinding{}, fmt.Errorf("iso8601 requires a time.Time field")
		}
		injector = func(base unsafe.Pointer, row int, val []byte, errOut *RowError) bool {
			ptr := (*string)(unsafe.Add(base, offset))
			*ptr = unsafe.String(unsafe.SliceData(val), len(val))
			return true
		}
		extractor = func(base unsafe.Pointer, dst []byte) []byte {
			val := *(*string)(unsafe.Add(base, offset))
			return append(dst, val...)
		}
		isZero = func(base unsafe.Pointer) bool {
			return *(*string)(unsafe.Add(base, offset)) == ""
		}
		detach = func(base unsafe.Pointer) {
			ptr := (*string)(unsafe.Add(base, offset))
			*ptr = strings.Clone(*ptr)
		}
	case field.Type == reflect.TypeOf(int64(0)):
		if isISO8601 {
			return FieldBinding{}, fmt.Errorf("iso8601 requires a time.Time field")
		}
		if !isCurrency {
			return FieldBinding{}, fmt.Errorf("int64 requires currency")
		}
		injector = func(base unsafe.Pointer, row int, val []byte, errOut *RowError) bool {
			if len(val) == 0 {
				return true
			}
			cents, err := scalars.ParseCurrencyCents(val)
			if err != nil {
				*errOut = RowError{Row: row, Col: name, Code: "invalid_currency", Expected: "ISO 4217 Currency", Actual: string(val), Message: err.Error()}
				return false
			}
			*(*int64)(unsafe.Add(base, offset)) = cents
			return true
		}
		extractor = func(base unsafe.Pointer, dst []byte) []byte {
			cents := *(*int64)(unsafe.Add(base, offset))
			return scalars.AppendCurrencyCents(dst, cents)
		}
		isZero = func(base unsafe.Pointer) bool {
			return *(*int64)(unsafe.Add(base, offset)) == 0
		}
		detach = func(base unsafe.Pointer) {}
	case field.Type == reflect.TypeOf(time.Time{}):
		if isCurrency {
			return FieldBinding{}, fmt.Errorf("currency requires an int64 field")
		}
		if !isISO8601 {
			return FieldBinding{}, fmt.Errorf("time.Time requires iso8601")
		}
		injector = func(base unsafe.Pointer, row int, val []byte, errOut *RowError) bool {
			if len(val) == 0 {
				return true
			}
			ts, err := scalars.ParseISO8601(val)
			if err != nil {
				*errOut = RowError{Row: row, Col: name, Code: "invalid_iso8601", Expected: "YYYY-MM-DDTHH:MM:SSZ", Actual: string(val), Message: err.Error()}
				return false
			}
			*(*time.Time)(unsafe.Add(base, offset)) = ts
			return true
		}
		extractor = func(base unsafe.Pointer, dst []byte) []byte {
			ts := *(*time.Time)(unsafe.Add(base, offset))
			return scalars.AppendISO8601(dst, ts)
		}
		isZero = func(base unsafe.Pointer) bool {
			return (*(*time.Time)(unsafe.Add(base, offset))).IsZero()
		}
		detach = func(base unsafe.Pointer) {}
	default:
		return FieldBinding{}, fmt.Errorf("unsupported type %s", field.Type)
	}

	return FieldBinding{
		ColIndex: colIdx,
		KeyName:  keyName,
		KeyBytes: []byte(keyName),
		Required: required,
		Name:     name,
		Offset:   offset,
		Inject:   injector,
		Extract:  extractor,
		IsZero:   isZero,
		Detach:   detach,
	}, nil
}

func parseColIndex(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty col index")
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("invalid col index %q", s)
		}
	}
	idx, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid col index %q", s)
	}
	return idx, nil
}

// ResetAndBind zeroes out the target struct to prevent data bleed across iterations.
func (s *Schema[T]) ResetAndBind(target *T) {
	*target = s.zero
}

// SetField injects val into target using either positional col index or key name.
func (s *Schema[T]) SetField(target *T, row int, col int, key []byte, val []byte, errOut *RowError) bool {
	base := unsafe.Pointer(target)
	if col >= 0 && col < len(s.colBindings) {
		b := &s.colBindings[col]
		if b.Inject != nil {
			return b.Inject(base, row, val, errOut)
		}
	}
	if len(key) > 0 {
		for i := range s.keyBindings {
			b := &s.keyBindings[i]
			if bytes.Equal(b.KeyBytes, key) {
				return b.Inject(base, row, val, errOut)
			}
		}
	}
	return true
}

// ValidateRequired verifies that all required fields have non-zero values.
func (s *Schema[T]) ValidateRequired(target *T, row int, errsOut []RowError) []RowError {
	base := unsafe.Pointer(target)
	errsOut = errsOut[:0]

	for i := range s.required {
		req := &s.required[i]
		if req.IsZero(base) {
			errsOut = append(errsOut, RowError{
				Row:     row,
				Col:     requiredLabel(req),
				Code:    "required_field",
				Message: "Field is required",
			})
		}
	}
	return errsOut
}

func requiredLabel(b *FieldBinding) string {
	if b.KeyName != "" {
		return b.KeyName
	}
	if b.ColIndex >= 0 {
		return "col:" + strconv.Itoa(b.ColIndex)
	}
	return b.Name
}
