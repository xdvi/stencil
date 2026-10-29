package memory

import (
	"sync"
	"unsafe"
)

// RetentionPolicy defines the memory retention and lifecycle policy for decoded strings.
type RetentionPolicy uint8

const (
	// PolicyEphemeral uses zero-copy string aliasing over the underlying input buffer.
	// Strings are only valid during the lifetime of the current row visitor callback.
	PolicyEphemeral RetentionPolicy = iota

	// PolicyArenaSlab copies strings into contiguous memory slabs allocated by a StringArena.
	// Strings remain valid until StringArena.Reset() is explicitly called for the batch or stream.
	PolicyArenaSlab

	// PolicyInterned deduplicates strings through a thread-safe canonicalization pool.
	// Recommended for high-cardinality repeat columns (statuses, countries, currencies, codes).
	PolicyInterned
)

// String returns a human-readable name for the retention policy.
func (p RetentionPolicy) String() string {
	switch p {
	case PolicyEphemeral:
		return "PolicyEphemeral"
	case PolicyArenaSlab:
		return "PolicyArenaSlab"
	case PolicyInterned:
		return "PolicyInterned"
	default:
		return "PolicyUnknown"
	}
}

// DefaultSlabSize is the default byte capacity of each memory slab chunk (64 KB).
const DefaultSlabSize = 64 * 1024

// StringArena is a slab-based bump allocator designed for high-throughput string copies
// with zero GC fragmentation and amortized zero allocations across reuse cycles.
type StringArena struct {
	slabSize  int
	slabs     [][]byte
	slabIdx   int
	offset    int
	oversized [][]byte
	allocated int64
}

// NewStringArena creates a new StringArena with the given slab size.
// If slabSize <= 0, DefaultSlabSize (64 KB) is used.
func NewStringArena(slabSize int) *StringArena {
	if slabSize <= 0 {
		slabSize = DefaultSlabSize
	}
	firstSlab := make([]byte, slabSize)
	return &StringArena{
		slabSize: slabSize,
		slabs:    [][]byte{firstSlab},
		slabIdx:  0,
		offset:   0,
	}
}

// Alloc copies the given byte slice into the arena slab and returns a persistent string.
// If len(b) == 0, it returns an empty string without allocation.
func (a *StringArena) Alloc(b []byte) string {
	n := len(b)
	if n == 0 {
		return ""
	}

	a.allocated += int64(n)

	// If the string is larger than a standard slab, allocate an oversized buffer.
	if n > a.slabSize {
		buf := make([]byte, n)
		copy(buf, b)
		a.oversized = append(a.oversized, buf)
		return unsafe.String(unsafe.SliceData(buf), n)
	}

	current := a.slabs[a.slabIdx]
	if a.offset+n > len(current) {
		// Need next slab
		a.slabIdx++
		if a.slabIdx >= len(a.slabs) {
			a.slabs = append(a.slabs, make([]byte, a.slabSize))
		}
		current = a.slabs[a.slabIdx]
		a.offset = 0
	}

	dest := current[a.offset : a.offset+n]
	copy(dest, b)
	a.offset += n

	return unsafe.String(unsafe.SliceData(dest), n)
}

// AllocString copies the given string into the arena slab and returns the persistent copy.
func (a *StringArena) AllocString(s string) string {
	if len(s) == 0 {
		return ""
	}
	return a.Alloc(unsafe.Slice(unsafe.StringData(s), len(s)))
}

// Reset rewinds the arena bump pointers to reuse all allocated slabs without heap churn.
func (a *StringArena) Reset() {
	a.slabIdx = 0
	a.offset = 0
	a.allocated = 0
	if len(a.oversized) > 0 {
		clear(a.oversized)
		a.oversized = a.oversized[:0]
	}
}

// BytesAllocated returns the cumulative byte count of strings allocated in the current cycle.
func (a *StringArena) BytesAllocated() int64 {
	return a.allocated
}

// TotalCapacity returns the total bytes held across all allocated slabs.
func (a *StringArena) TotalCapacity() int64 {
	var total int64
	for _, s := range a.slabs {
		total += int64(len(s))
	}
	for _, o := range a.oversized {
		total += int64(len(o))
	}
	return total
}

// SlabCount returns the number of fixed slabs currently retained by the arena.
func (a *StringArena) SlabCount() int {
	return len(a.slabs)
}

// InternPool provides thread-safe string interning to deduplicate high-frequency strings.
type InternPool struct {
	mu    sync.RWMutex
	table map[string]string
}

// NewInternPool creates a new thread-safe string interning pool.
func NewInternPool() *InternPool {
	return &InternPool{
		table: make(map[string]string, 256),
	}
}

// Intern returns a canonical deduplicated string for the given byte slice.
// If b is already present in the pool, no heap allocation occurs.
func (p *InternPool) Intern(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	// Fast read path: stack string over borrowed bytes without allocation for map lookup
	key := unsafe.String(unsafe.SliceData(b), len(b))

	p.mu.RLock()
	canon, ok := p.table[key]
	p.mu.RUnlock()
	if ok {
		return canon
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-checked locking
	if canon, ok := p.table[key]; ok {
		return canon
	}

	// Allocate permanent string for insertion
	canon = string(b)
	p.table[canon] = canon
	return canon
}

// InternString returns a canonical deduplicated string for the input string.
func (p *InternPool) InternString(s string) string {
	if len(s) == 0 {
		return ""
	}

	p.mu.RLock()
	canon, ok := p.table[s]
	p.mu.RUnlock()
	if ok {
		return canon
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if canon, ok := p.table[s]; ok {
		return canon
	}

	p.table[s] = s
	return s
}

// Len returns the number of unique strings interned in the pool.
func (p *InternPool) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.table)
}

// Clear removes all interned strings from the pool.
func (p *InternPool) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	clear(p.table)
}

// Allocator manages string lifecycle and retention according to a configured policy.
type Allocator struct {
	Policy RetentionPolicy
	Arena  *StringArena
	Intern *InternPool
}

// NewAllocator constructs an Allocator for the chosen RetentionPolicy.
// If policy requires Arena or Intern and they are nil, default instances are created.
func NewAllocator(policy RetentionPolicy, arena *StringArena, intern *InternPool) *Allocator {
	if policy == PolicyArenaSlab && arena == nil {
		arena = NewStringArena(DefaultSlabSize)
	}
	if policy == PolicyInterned && intern == nil {
		intern = NewInternPool()
	}
	return &Allocator{
		Policy: policy,
		Arena:  arena,
		Intern: intern,
	}
}

// CopyBytes produces a string from raw bytes conforming to the Allocator's policy.
func (a *Allocator) CopyBytes(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	switch a.Policy {
	case PolicyEphemeral:
		return unsafe.String(unsafe.SliceData(b), len(b))
	case PolicyArenaSlab:
		if a.Arena != nil {
			return a.Arena.Alloc(b)
		}
		return string(b)
	case PolicyInterned:
		if a.Intern != nil {
			return a.Intern.Intern(b)
		}
		return string(b)
	default:
		return string(b)
	}
}

// Reset resets the underlying arena if PolicyArenaSlab is used.
func (a *Allocator) Reset() {
	if a.Arena != nil {
		a.Arena.Reset()
	}
}
