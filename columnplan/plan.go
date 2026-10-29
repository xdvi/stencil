package columnplan

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"unsafe"

	"github.com/xdvi/stencil"
	"github.com/xdvi/stencil/binder"
)

// ErrMissingRequiredColumn is returned during Phase 1 if a required schema field is missing in headers.
var ErrMissingRequiredColumn = errors.New("columnplan: missing required column in header")

// ErrDuplicateHeaderColumn is returned if a column name appears multiple times in the header.
var ErrDuplicateHeaderColumn = errors.New("columnplan: duplicate column in header")

// JumpEntry represents a pre-compiled, direct-indexed jump target for a physical column position.
type JumpEntry struct {
	FieldIdx int32                 // -1 if unmapped
	Name     string                // Canonical field/column name
	Inject   stencil.FieldInjector // Direct injector func
	Required bool
}

// ColumnPlan represents a pre-compiled two-phase column mapping.
// Phase 1 resolves header permutations once.
// Phase 2 executes direct array indexing jump table lookups with zero allocation.
type ColumnPlan[T any] struct {
	schema        *stencil.Schema[T]
	jumpTable     []JumpEntry
	unmapped      []string
	mappedCount   int
	requiredCount int
}

// CompileHeaderPlan analyzes header column bytes and compiles a Phase 2 jump table.
func CompileHeaderPlan[T any](headers [][]byte, s *stencil.Schema[T]) (*ColumnPlan[T], error) {
	if len(headers) == 0 {
		return nil, errors.New("columnplan: header row cannot be empty")
	}
	if s == nil {
		return nil, errors.New("columnplan: schema cannot be nil")
	}

	allBindings := s.AllBindings()

	// Map available keys in schema to binding index
	var keyEntries []binder.KeyEntry
	for i, b := range allBindings {
		if len(b.KeyBytes) > 0 {
			keyEntries = append(keyEntries, binder.KeyEntry{Key: b.KeyBytes, FieldIdx: i})
		}
		// Also allow matching by struct field name
		keyEntries = append(keyEntries, binder.KeyEntry{Key: []byte(b.Name), FieldIdx: i})
	}

	keyTable, err := binder.NewKeyTable(keyEntries)
	if err != nil {
		return nil, fmt.Errorf("columnplan: failed to build key lookup: %w", err)
	}

	seenHeaders := make(map[string]bool, len(headers))
	jumpTable := make([]JumpEntry, len(headers))
	matchedBindings := make(map[int]bool, len(allBindings))
	var unmapped []string

	for colIdx, h := range headers {
		trimmed := bytes.TrimSpace(h)
		hStr := string(trimmed)

		if seenHeaders[hStr] {
			return nil, fmt.Errorf("%w: %q at column %d", ErrDuplicateHeaderColumn, hStr, colIdx)
		}
		seenHeaders[hStr] = true

		bindingIdx := keyTable.Lookup(trimmed)
		if bindingIdx < 0 {
			// Check positional col fallback if header matches "col:N"
			if bytes.HasPrefix(trimmed, []byte("col:")) {
				if n, err := strconv.Atoi(string(trimmed[4:])); err == nil {
					for bi, b := range allBindings {
						if b.ColIndex == n {
							bindingIdx = bi
							break
						}
					}
				}
			}
		}

		if bindingIdx >= 0 {
			b := &allBindings[bindingIdx]
			matchedBindings[bindingIdx] = true
			jumpTable[colIdx] = JumpEntry{
				FieldIdx: int32(bindingIdx),
				Name:     b.Name,
				Inject:   b.Inject,
				Required: b.Required,
			}
		} else {
			jumpTable[colIdx] = JumpEntry{
				FieldIdx: -1,
				Name:     hStr,
				Inject:   nil,
				Required: false,
			}
			unmapped = append(unmapped, hStr)
		}
	}

	// Verify all required schema fields were satisfied by headers
	for bi, b := range allBindings {
		if b.Required && !matchedBindings[bi] {
			label := b.KeyName
			if label == "" {
				label = b.Name
			}
			return nil, fmt.Errorf("%w: %q", ErrMissingRequiredColumn, label)
		}
	}

	mappedCount := len(headers) - len(unmapped)

	return &ColumnPlan[T]{
		schema:      s,
		jumpTable:   jumpTable,
		unmapped:    unmapped,
		mappedCount: mappedCount,
	}, nil
}

// CompileHeaderPlanStrings compiles a ColumnPlan from string column names.
func CompileHeaderPlanStrings[T any](headers []string, s *stencil.Schema[T]) (*ColumnPlan[T], error) {
	byteHeaders := make([][]byte, len(headers))
	for i, h := range headers {
		byteHeaders[i] = []byte(h)
	}
	return CompileHeaderPlan(byteHeaders, s)
}

// ApplyCell injects the value of a physical column colIdx into target with direct index jump.
// Returns false if an injector error occurred.
func (p *ColumnPlan[T]) ApplyCell(target *T, row int, colIdx int, val []byte, errOut *stencil.RowError) bool {
	if colIdx < 0 || colIdx >= len(p.jumpTable) {
		return true // Ignore out of bounds physical columns
	}
	entry := &p.jumpTable[colIdx]
	if entry.FieldIdx >= 0 && entry.Inject != nil {
		base := unsafe.Pointer(target)
		return entry.Inject(base, row, val, errOut)
	}
	return true
}

// ValidateRequired delegates required field checking to the underlying schema.
func (p *ColumnPlan[T]) ValidateRequired(target *T, row int, errsOut []stencil.RowError) []stencil.RowError {
	return p.schema.ValidateRequired(target, row, errsOut)
}

// JumpTable returns the raw slice of jump targets.
func (p *ColumnPlan[T]) JumpTable() []JumpEntry {
	return p.jumpTable
}

// MappedCount returns the number of physical columns mapped to schema fields.
func (p *ColumnPlan[T]) MappedCount() int {
	return p.mappedCount
}

// TotalColumns returns the total physical columns in the header.
func (p *ColumnPlan[T]) TotalColumns() int {
	return len(p.jumpTable)
}

// UnmappedColumns returns the list of header column names not bound to any schema field.
func (p *ColumnPlan[T]) UnmappedColumns() []string {
	return p.unmapped
}
