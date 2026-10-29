package memory_test

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
	"unsafe"

	"github.com/xdvi/stencil/memory"
)

func TestRetentionPolicy_String(t *testing.T) {
	cases := []struct {
		policy memory.RetentionPolicy
		want   string
	}{
		{memory.PolicyEphemeral, "PolicyEphemeral"},
		{memory.PolicyArenaSlab, "PolicyArenaSlab"},
		{memory.PolicyInterned, "PolicyInterned"},
		{memory.RetentionPolicy(99), "PolicyUnknown"},
	}

	for _, tc := range cases {
		if got := tc.policy.String(); got != tc.want {
			t.Errorf("expected %s, got %s", tc.want, got)
		}
	}
}

func TestStringArena_AllocAndCapacity(t *testing.T) {
	arena := memory.NewStringArena(128)

	if arena.SlabCount() != 1 {
		t.Fatalf("expected 1 slab, got %d", arena.SlabCount())
	}
	if arena.BytesAllocated() != 0 {
		t.Fatalf("expected 0 bytes allocated, got %d", arena.BytesAllocated())
	}

	// Empty string allocation
	if empty := arena.Alloc(nil); empty != "" {
		t.Errorf("expected empty string, got %q", empty)
	}

	// Allocate within first slab
	s1 := arena.Alloc([]byte("hello world"))
	if s1 != "hello world" {
		t.Fatalf("expected 'hello world', got %q", s1)
	}
	if arena.BytesAllocated() != int64(len("hello world")) {
		t.Fatalf("expected %d bytes, got %d", len("hello world"), arena.BytesAllocated())
	}

	// Force allocation to exceed first slab (128 bytes)
	var words []string
	for i := 0; i < 20; i++ {
		str := fmt.Sprintf("record-chunk-payload-%04d", i)
		words = append(words, arena.AllocString(str))
	}

	if arena.SlabCount() <= 1 {
		t.Errorf("expected arena to expand beyond 1 slab, got %d", arena.SlabCount())
	}

	// Verify all allocated strings remain intact and independent
	for i, w := range words {
		expected := fmt.Sprintf("record-chunk-payload-%04d", i)
		if w != expected {
			t.Errorf("string corrupted at %d: expected %q, got %q", i, expected, w)
		}
	}

	// Test oversized allocation (> slabSize 128)
	oversizedBytes := bytes.Repeat([]byte("X"), 300)
	huge := arena.Alloc(oversizedBytes)
	if len(huge) != 300 || huge[0] != 'X' {
		t.Errorf("failed to allocate oversized string properly")
	}

	if arena.TotalCapacity() < 300+128 {
		t.Errorf("expected total capacity >= 428, got %d", arena.TotalCapacity())
	}
}

func TestStringArena_ResetZeroAlloc(t *testing.T) {
	arena := memory.NewStringArena(1024)

	for i := 0; i < 100; i++ {
		arena.AllocString(fmt.Sprintf("entry-%d", i))
	}

	initialSlabs := arena.SlabCount()
	arena.Reset()

	if arena.BytesAllocated() != 0 {
		t.Errorf("expected 0 bytes allocated after Reset, got %d", arena.BytesAllocated())
	}

	// Allocations after reset should reuse slabs without increasing slab count
	allocs := testing.AllocsPerRun(10, func() {
		arena.Reset()
		for i := 0; i < 50; i++ {
			_ = arena.AllocString("test-string-reuse")
		}
	})

	if arena.SlabCount() != initialSlabs {
		t.Errorf("expected slab count to remain %d, got %d", initialSlabs, arena.SlabCount())
	}

	if allocs > 0 {
		t.Logf("Notice: allocs per run: %v", allocs)
	}
}

func TestInternPool_Deduplication(t *testing.T) {
	pool := memory.NewInternPool()

	if pool.Len() != 0 {
		t.Fatalf("expected empty pool, got %d", pool.Len())
	}
	if pool.Intern(nil) != "" {
		t.Errorf("expected empty string from nil")
	}
	if pool.InternString("") != "" {
		t.Errorf("expected empty string from empty string")
	}

	b1 := []byte("active_status")
	b2 := []byte("active_status")

	s1 := pool.Intern(b1)
	s2 := pool.Intern(b2)

	if s1 != "active_status" || s2 != "active_status" {
		t.Fatalf("unexpected interned values: %q, %q", s1, s2)
	}

	// Verify exact memory pointer equivalence (true deduplication)
	ptr1 := unsafe.StringData(s1)
	ptr2 := unsafe.StringData(s2)
	if ptr1 != ptr2 {
		t.Fatalf("expected identical string data pointers: %p != %p", ptr1, ptr2)
	}

	if pool.Len() != 1 {
		t.Fatalf("expected pool length 1, got %d", pool.Len())
	}

	// InternString lookup
	s3 := pool.InternString("active_status")
	if unsafe.StringData(s3) != ptr1 {
		t.Fatalf("expected InternString to match interned pointer")
	}

	// Another unique key
	_ = pool.InternString("pending_status")
	if pool.Len() != 2 {
		t.Fatalf("expected pool length 2, got %d", pool.Len())
	}

	pool.Clear()
	if pool.Len() != 0 {
		t.Fatalf("expected 0 after Clear, got %d", pool.Len())
	}
}

func TestInternPool_ConcurrentAccess(t *testing.T) {
	pool := memory.NewInternPool()
	var wg sync.WaitGroup

	keys := []string{"PENDING", "ACTIVE", "CANCELED", "REFUNDED", "FAILED"}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				k := keys[(id+j)%len(keys)]
				res := pool.Intern([]byte(k))
				if res != k {
					t.Errorf("expected %s, got %s", k, res)
				}
			}
		}(i)
	}

	wg.Wait()

	if pool.Len() != len(keys) {
		t.Fatalf("expected %d unique keys, got %d", len(keys), pool.Len())
	}
}

func TestAllocator_Policies(t *testing.T) {
	// 1. Ephemeral
	allocEphemeral := memory.NewAllocator(memory.PolicyEphemeral, nil, nil)
	src := []byte("transient")
	res1 := allocEphemeral.CopyBytes(src)
	if res1 != "transient" {
		t.Errorf("ephemeral: expected 'transient', got %q", res1)
	}

	// 2. Arena Slab
	allocArena := memory.NewAllocator(memory.PolicyArenaSlab, nil, nil)
	res2 := allocArena.CopyBytes([]byte("arena-data"))
	if res2 != "arena-data" {
		t.Errorf("arena: expected 'arena-data', got %q", res2)
	}
	if allocArena.Arena.BytesAllocated() != int64(len("arena-data")) {
		t.Errorf("expected %d bytes in arena", len("arena-data"))
	}
	allocArena.Reset()
	if allocArena.Arena.BytesAllocated() != 0 {
		t.Errorf("expected 0 bytes after Reset")
	}

	// 3. Interned
	allocIntern := memory.NewAllocator(memory.PolicyInterned, nil, nil)
	k1 := allocIntern.CopyBytes([]byte("USD"))
	k2 := allocIntern.CopyBytes([]byte("USD"))
	if unsafe.StringData(k1) != unsafe.StringData(k2) {
		t.Fatalf("expected identical pointer for interned values")
	}
}

func BenchmarkStringArena_Alloc(b *testing.B) {
	arena := memory.NewStringArena(64 * 1024)
	payload := []byte("customer-reference-0123456789-abcdef")

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if (i & 1023) == 0 {
			arena.Reset()
		}
		_ = arena.Alloc(payload)
	}
}

func BenchmarkInternPool_Hit(b *testing.B) {
	pool := memory.NewInternPool()
	key := []byte("PROCESSING_STATE_CODE")
	_ = pool.Intern(key)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = pool.Intern(key)
	}
}
