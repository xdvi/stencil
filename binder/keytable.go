package binder

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"unsafe"
)

// ErrDuplicateKey is returned when the same key is registered multiple times in a KeyTable.
var ErrDuplicateKey = errors.New("binder: duplicate key in table")

// KeyEntry describes a key and its target field index for compilation.
type KeyEntry struct {
	Key      []byte
	FieldIdx int
}

// KeySlot is a cache-line friendly slot in the flat hash table.
type KeySlot struct {
	Prefix   uint64 // 64-bit prefix of key in little-endian (zero-padded if < 8 bytes)
	Hash     uint32 // Fast 32-bit hash
	KeyLen   uint16 // Byte length of key
	FieldIdx int32  // -1 if slot is empty, otherwise compiled field index
	KeyBytes []byte // Full key bytes for collision verification
}

// KeyTable is an L1-cache resident, open-addressing hash table optimized for
// zero-allocation, single-probe O(1) key dispatching in streaming tokenizers.
type KeyTable struct {
	slots []KeySlot
	mask  uint32
	count int
}

// LoadPrefix64 reads up to 8 bytes of key into a uint64 in little-endian order.
// Keys shorter than 8 bytes are zero-padded.
func LoadPrefix64(b []byte) uint64 {
	n := len(b)
	if n >= 8 {
		return binary.LittleEndian.Uint64(b)
	}
	var buf [8]byte
	copy(buf[:], b)
	return binary.LittleEndian.Uint64(buf[:])
}

// HashKey computes an inline 32-bit FNV-1a hash over raw key bytes.
func HashKey(b []byte) uint32 {
	const (
		offset32 = 2166136261
		prime32  = 16777619
	)
	h := uint32(offset32)
	for i := 0; i < len(b); i++ {
		h = (h ^ uint32(b[i])) * prime32
	}
	return h
}

// NewKeyTable compiles entries into a flat power-of-two table with a max load factor of 50%.
func NewKeyTable(entries []KeyEntry) (*KeyTable, error) {
	if len(entries) == 0 {
		return &KeyTable{slots: nil, mask: 0, count: 0}, nil
	}

	// Calculate capacity: smallest power of 2 with load factor <= 50%, min 16
	desired := len(entries) * 2
	if desired < 16 {
		desired = 16
	}
	capPow2 := 1 << (32 - bits.LeadingZeros32(uint32(desired-1)))

	slots := make([]KeySlot, capPow2)
	for i := range slots {
		slots[i].FieldIdx = -1
	}

	mask := uint32(capPow2 - 1)
	table := &KeyTable{
		slots: slots,
		mask:  mask,
		count: 0,
	}

	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if len(entry.Key) == 0 {
			return nil, errors.New("binder: key must not be empty")
		}
		strKey := string(entry.Key)
		if seen[strKey] {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateKey, strKey)
		}
		seen[strKey] = true

		table.insert(entry.Key, int32(entry.FieldIdx))
	}

	return table, nil
}

func (t *KeyTable) insert(key []byte, fieldIdx int32) {
	h := HashKey(key)
	prefix := LoadPrefix64(key)
	klen := uint16(len(key))

	idx := h & t.mask
	for {
		slot := &t.slots[idx]
		if slot.FieldIdx < 0 {
			// Found empty slot
			slot.Prefix = prefix
			slot.Hash = h
			slot.KeyLen = klen
			slot.FieldIdx = fieldIdx
			slot.KeyBytes = key
			t.count++
			return
		}
		idx = (idx + 1) & t.mask
	}
}

// Lookup finds the field index for key with zero allocations in O(1) time.
// Returns -1 if key is not found.
func (t *KeyTable) Lookup(key []byte) int {
	if len(key) == 0 || len(t.slots) == 0 {
		return -1
	}

	klen := uint16(len(key))
	prefix := LoadPrefix64(key)
	h := HashKey(key)

	idx := h & t.mask
	for {
		slot := &t.slots[idx]
		if slot.FieldIdx < 0 {
			return -1
		}

		// Fast filter: length, prefix, and hash check before byte comparison
		if slot.KeyLen == klen && slot.Prefix == prefix && slot.Hash == h {
			if bytes.Equal(slot.KeyBytes, key) {
				return int(slot.FieldIdx)
			}
		}

		idx = (idx + 1) & t.mask
	}
}

// LookupString finds the field index for a string key with zero allocations.
func (t *KeyTable) LookupString(key string) int {
	if len(key) == 0 {
		return -1
	}
	return t.Lookup(unsafe.Slice(unsafe.StringData(key), len(key)))
}

// Len returns the number of active entries in the table.
func (t *KeyTable) Len() int {
	return t.count
}

// Capacity returns the total allocated slot capacity.
func (t *KeyTable) Capacity() int {
	return len(t.slots)
}
