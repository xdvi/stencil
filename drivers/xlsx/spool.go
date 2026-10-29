package xlsx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"os"
	"sync"
)

// DefaultRAMThreshold is the default memory cap (5 MB) held before spilling to disk.
const DefaultRAMThreshold int64 = 5 * 1024 * 1024

var ramPool = sync.Pool{
	New: func() any {
		b := make([]byte, DefaultRAMThreshold)
		return &b
	},
}

// SpoolOption configures a HybridSpoolReaderAt.
type SpoolOption func(*spoolOptions)

type spoolOptions struct {
	ramThreshold int64
	maxBytes     int64
	ctx          context.Context
}

// WithSpoolRAMThreshold configures the maximum RAM to allocate before disk spill.
func WithSpoolRAMThreshold(n int64) SpoolOption {
	return func(o *spoolOptions) {
		if n > 0 {
			o.ramThreshold = n
		} else {
			o.ramThreshold = 0
		}
	}
}

// WithSpoolMaxBytes caps total bytes accepted from source.
func WithSpoolMaxBytes(n int64) SpoolOption {
	return func(o *spoolOptions) {
		if n > 0 {
			o.maxBytes = n
		} else {
			o.maxBytes = 0
		}
	}
}

// WithSpoolContext associates a cancellation context with spooling.
func WithSpoolContext(ctx context.Context) SpoolOption {
	return func(o *spoolOptions) {
		if ctx != nil {
			o.ctx = ctx
		}
	}
}

// HybridSpoolReaderAt buffers up to 5MB in pooled RAM for fast zero-disk execution,
// seamlessly spilling to a temporary disk file if payload exceeds the threshold.
type HybridSpoolReaderAt struct {
	mu           sync.Mutex
	pooledBuf    *[]byte
	memData      []byte
	tmpFile      *os.File
	tmpPath      string
	size         int64
	readPos      int64
	closed       bool
	isOnDisk     bool
	ramThreshold int64
}

// NewHybridSpoolReaderAt reads r into pooled RAM, spilling to disk if it exceeds ramThreshold.
func NewHybridSpoolReaderAt(r io.Reader, opts ...SpoolOption) (*HybridSpoolReaderAt, error) {
	cfg := spoolOptions{
		ramThreshold: DefaultRAMThreshold,
		maxBytes:     defaultMaxBytes,
		ctx:          context.Background(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	spool := &HybridSpoolReaderAt{
		ramThreshold: cfg.ramThreshold,
	}

	src := &contextReader{ctx: cfg.ctx, r: r}
	var reader io.Reader = src
	if cfg.maxBytes > 0 && cfg.maxBytes < math.MaxInt64 {
		reader = io.LimitReader(src, cfg.maxBytes+1)
	}

	// If RAM threshold is 0, spill directly to disk
	if cfg.ramThreshold <= 0 {
		if err := spool.spillToDiskAndCopy(reader, nil, cfg.maxBytes); err != nil {
			return nil, err
		}
		return spool, nil
	}

	// Borrow buffer from pool
	var bufPtr *[]byte
	if cfg.ramThreshold == DefaultRAMThreshold {
		bufPtr = ramPool.Get().(*[]byte)
	} else {
		b := make([]byte, cfg.ramThreshold)
		bufPtr = &b
	}
	spool.pooledBuf = bufPtr
	buffer := *bufPtr

	var bytesRead int64
	var hitEOF bool

	for bytesRead < cfg.ramThreshold {
		if err := cfg.ctx.Err(); err != nil {
			spool.returnPoolBuffer()
			return nil, err
		}

		n, err := reader.Read(buffer[bytesRead:])
		bytesRead += int64(n)
		if err == io.EOF {
			hitEOF = true
			break
		}
		if err != nil {
			spool.returnPoolBuffer()
			return nil, err
		}
	}

	// If entire stream fit in RAM
	if hitEOF {
		if cfg.maxBytes > 0 && bytesRead > cfg.maxBytes {
			spool.returnPoolBuffer()
			return nil, ErrFileTooLarge
		}
		spool.memData = buffer[:bytesRead]
		spool.size = bytesRead
		spool.isOnDisk = false
		return spool, nil
	}

	// Need to check if next byte is EOF or if we exceeded RAM
	var peekBuf [1]byte
	n, err := reader.Read(peekBuf[:])
	if err == io.EOF || (n == 0 && err == nil) {
		// Exactly fit in RAM
		if cfg.maxBytes > 0 && bytesRead > cfg.maxBytes {
			spool.returnPoolBuffer()
			return nil, ErrFileTooLarge
		}
		spool.memData = buffer[:bytesRead]
		spool.size = bytesRead
		spool.isOnDisk = false
		return spool, nil
	}
	if err != nil && err != io.EOF {
		spool.returnPoolBuffer()
		return nil, err
	}

	// Stream exceeds RAM threshold -> spill to disk!
	bufferedBytes := buffer[:bytesRead]
	spillReader := io.MultiReader(bytes.NewReader(peekBuf[:n]), reader)

	if err := spool.spillToDiskAndCopy(spillReader, bufferedBytes, cfg.maxBytes); err != nil {
		spool.returnPoolBuffer()
		return nil, err
	}

	// RAM buffer is no longer needed once spilled
	spool.returnPoolBuffer()
	return spool, nil
}

func (s *HybridSpoolReaderAt) returnPoolBuffer() {
	if s.pooledBuf != nil {
		if len(*s.pooledBuf) == int(DefaultRAMThreshold) {
			ramPool.Put(s.pooledBuf)
		}
		s.pooledBuf = nil
		s.memData = nil
	}
}

func (s *HybridSpoolReaderAt) spillToDiskAndCopy(r io.Reader, initialData []byte, maxBytes int64) error {
	tmp, err := os.CreateTemp("", "stencil-xlsx-spool-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	var total int64
	if len(initialData) > 0 {
		n, err := tmp.Write(initialData)
		total += int64(n)
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
			return err
		}
	}

	n, err := io.Copy(tmp, r)
	total += n
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}

	if maxBytes > 0 && total > maxBytes {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return ErrFileTooLarge
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}

	s.tmpFile = tmp
	s.tmpPath = tmpName
	s.size = total
	s.isOnDisk = true
	return nil
}

// Read implements io.Reader.
func (s *HybridSpoolReaderAt) Read(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, errors.New("xlsx: HybridSpoolReaderAt is closed")
	}

	if s.isOnDisk {
		return s.tmpFile.Read(p)
	}

	if s.readPos >= int64(len(s.memData)) {
		return 0, io.EOF
	}

	n = copy(p, s.memData[s.readPos:])
	s.readPos += int64(n)
	return n, nil
}

// ReadAt implements io.ReaderAt.
func (s *HybridSpoolReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, errors.New("xlsx: HybridSpoolReaderAt is closed")
	}

	if s.isOnDisk {
		return s.tmpFile.ReadAt(p, off)
	}

	if off < 0 || off >= int64(len(s.memData)) {
		return 0, io.EOF
	}

	n = copy(p, s.memData[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// Seek implements io.Seeker.
func (s *HybridSpoolReaderAt) Seek(offset int64, whence int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, errors.New("xlsx: HybridSpoolReaderAt is closed")
	}

	if s.isOnDisk {
		return s.tmpFile.Seek(offset, whence)
	}

	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = s.readPos + offset
	case io.SeekEnd:
		newPos = int64(len(s.memData)) + offset
	default:
		return 0, errors.New("xlsx: invalid whence")
	}

	if newPos < 0 {
		return 0, errors.New("xlsx: negative seek position")
	}

	s.readPos = newPos
	return newPos, nil
}

// Close cleans up resources, returning memory buffers to pool and removing temp disk files.
func (s *HybridSpoolReaderAt) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	var firstErr error
	s.returnPoolBuffer()

	if s.tmpFile != nil {
		if err := s.tmpFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		s.tmpFile = nil
	}

	if s.tmpPath != "" {
		if err := os.Remove(s.tmpPath); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
		s.tmpPath = ""
	}

	return firstErr
}

// IsOnDisk returns true if payload spilled to a temporary disk file.
func (s *HybridSpoolReaderAt) IsOnDisk() bool {
	return s.isOnDisk
}

// Size returns total bytes spooled.
func (s *HybridSpoolReaderAt) Size() int64 {
	return s.size
}

// TempPath returns the path to the disk file if spilled, or empty string if in RAM.
func (s *HybridSpoolReaderAt) TempPath() string {
	return s.tmpPath
}
