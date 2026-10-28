package schema

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/xdvi/stencil"
	"github.com/xdvi/stencil/scalars"
)

type Schema[T any] struct {
	colBindings []FieldBinding
	keyBindings []FieldBinding
	zero        T
	required    []FieldBinding
}

func Compile[T any]() (*Schema[T], error) {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil || t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("stencil: schema requires a struct type")
	}

	var cols []FieldBinding
	var keys []FieldBinding
	var reqs []FieldBinding

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("stencil")
		if tag == "" {
			continue
		}

		binding, err := parseFieldTag(field, tag)
		if err != nil {
			return nil, err
		}

		if binding.ColIndex >= 0 {
			for len(cols) <= binding.ColIndex {
				cols = append(cols, FieldBinding{ColIndex: -1})
			}
			cols[binding.ColIndex] = binding
		}
		if len(binding.KeyBytes) > 0 {
			keys = append(keys, binding)
		}
		if binding.Required {
			reqs = append(reqs, binding)
		}
	}

	return &Schema[T]{
		colBindings: cols,
		keyBindings: keys,
		zero:        zero,
		required:    reqs,
	}, nil
}

func parseFieldTag(field reflect.StructField, tag string) (FieldBinding, error) {
	parts := strings.Split(tag, ",")
	colIdx := -1
	var keyName string
	var required, isCurrency, isISO8601 bool

	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case strings.HasPrefix(p, "col:"):
			idx, err := strconv.Atoi(strings.TrimPrefix(p, "col:"))
			if err != nil || idx < 0 {
				return FieldBinding{}, fmt.Errorf("invalid col index in %q", tag)
			}
			colIdx = idx
		case strings.HasPrefix(p, "key:"):
			keyName = strings.TrimPrefix(p, "key:")
		case p == "required":
			required = true
		case p == "currency":
			isCurrency = true
		case p == "iso8601":
			isISO8601 = true
		}
	}

	offset := field.Offset
	name := field.Name
	var injector fieldInjector
	var extractor fieldExtractor

	switch {
	case field.Type == reflect.TypeOf(""):
		injector = func(base unsafe.Pointer, row int, val []byte, errOut *stencil.RowError) bool {
			if len(val) == 0 && required {
				*errOut = stencil.RowError{Row: row, Col: name, Code: "required_field", Message: "Field is required"}
				return false
			}
			ptr := (*string)(unsafe.Add(base, offset))
			*ptr = unsafe.String(unsafe.SliceData(val), len(val))
			return true
		}
		extractor = func(base unsafe.Pointer, dst []byte) []byte {
			val := *(*string)(unsafe.Add(base, offset))
			return append(dst, val...)
		}
	case field.Type == reflect.TypeOf(int64(0)) && isCurrency:
		injector = func(base unsafe.Pointer, row int, val []byte, errOut *stencil.RowError) bool {
			if len(val) == 0 {
				if required {
					*errOut = stencil.RowError{Row: row, Col: name, Code: "required_field", Message: "Field is required"}
					return false
				}
				return true
			}
			cents, err := scalars.ParseCurrencyCents(val)
			if err != nil {
				*errOut = stencil.RowError{Row: row, Col: name, Code: "invalid_currency", Expected: "ISO 4217 Currency", Actual: string(val), Message: err.Error()}
				return false
			}
			*(*int64)(unsafe.Add(base, offset)) = cents
			return true
		}
		extractor = func(base unsafe.Pointer, dst []byte) []byte {
			cents := *(*int64)(unsafe.Add(base, offset))
			return scalars.AppendCurrencyCents(dst, cents)
		}
	case field.Type == reflect.TypeOf(time.Time{}) && isISO8601:
		injector = func(base unsafe.Pointer, row int, val []byte, errOut *stencil.RowError) bool {
			if len(val) == 0 {
				if required {
					*errOut = stencil.RowError{Row: row, Col: name, Code: "required_field", Message: "Field is required"}
					return false
				}
				return true
			}
			ts, err := scalars.ParseISO8601(val)
			if err != nil {
				*errOut = stencil.RowError{Row: row, Col: name, Code: "invalid_iso8601", Expected: "YYYY-MM-DDTHH:MM:SSZ", Actual: string(val), Message: err.Error()}
				return false
			}
			*(*time.Time)(unsafe.Add(base, offset)) = ts
			return true
		}
		extractor = func(base unsafe.Pointer, dst []byte) []byte {
			ts := *(*time.Time)(unsafe.Add(base, offset))
			return scalars.AppendISO8601(dst, ts)
		}
	default:
		return FieldBinding{}, fmt.Errorf("stencil: unsupported type %s for field %s", field.Type, field.Name)
	}

	return FieldBinding{
		ColIndex: colIdx,
		KeyName:  keyName,
		KeyBytes: []byte(keyName),
		Required: required,
		Offset:   offset,
		Inject:   injector,
		Extract:  extractor,
	}, nil
}

func (s *Schema[T]) ResetAndBind(target *T) {
	*target = s.zero
}

func (s *Schema[T]) SetField(target *T, row int, col int, key []byte, val []byte, errOut *stencil.RowError) bool {
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

func (s *Schema[T]) ValidateRequired(target *T, row int, errsOut []stencil.RowError) []stencil.RowError {
	base := unsafe.Pointer(target)
	errsOut = errsOut[:0]
	var rowErr stencil.RowError

	for i := range s.required {
		req := &s.required[i]
		switch {
		case *(*string)(unsafe.Add(base, req.Offset)) == "":
			rowErr = stencil.RowError{Row: row, Col: req.KeyName, Code: "required_field", Message: "Field is required"}
			errsOut = append(errsOut, rowErr)
		}
	}
	return errsOut
}
