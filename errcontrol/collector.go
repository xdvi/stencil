package errcontrol

import (
	"errors"
	"fmt"
	"sync"

	"github.com/xdvi/stencil"
)

// ErrCircuitBreakerTripped is returned when the error collector trips and halts ingestion.
var ErrCircuitBreakerTripped = errors.New("errcontrol: circuit breaker tripped")

// CollectorOption configures the ErrorCollector.
type CollectorOption func(*collectorConfig)

type collectorConfig struct {
	maxErrors          int
	consecutiveLimit   int
	errorRateThreshold float64
	minRateSamples     int
}

// WithMaxErrors sets the hard bound on how many errors are stored in memory (default 100).
func WithMaxErrors(limit int) CollectorOption {
	return func(c *collectorConfig) {
		if limit > 0 {
			c.maxErrors = limit
		}
	}
}

// WithConsecutiveLimit sets how many consecutive row errors trip the circuit breaker (default 50).
func WithConsecutiveLimit(n int) CollectorOption {
	return func(c *collectorConfig) {
		if n > 0 {
			c.consecutiveLimit = n
		}
	}
}

// WithErrorRateThreshold sets a maximum error rate (0.0 to 1.0) after minSamples rows processed.
func WithErrorRateThreshold(rate float64, minSamples int) CollectorOption {
	return func(c *collectorConfig) {
		if rate > 0.0 && rate <= 1.0 {
			c.errorRateThreshold = rate
		}
		if minSamples > 0 {
			c.minRateSamples = minSamples
		}
	}
}

// ErrorCollector provides bounded memory storage and circuit breaker protection
// for streaming row errors, preventing OOM and cascading failures on corrupt inputs.
type ErrorCollector struct {
	mu                 sync.Mutex
	cfg                collectorConfig
	errors             []stencil.RowError
	totalErrors        int64
	totalRows          int64
	consecutiveErrors  int
	circuitTripped     bool
	tripReason         string
}

// NewCollector constructs an ErrorCollector with the given options.
func NewCollector(opts ...CollectorOption) *ErrorCollector {
	cfg := collectorConfig{
		maxErrors:          100,
		consecutiveLimit:   50,
		errorRateThreshold: 0,
		minRateSamples:     100,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &ErrorCollector{
		cfg:    cfg,
		errors: make([]stencil.RowError, 0, cfg.maxErrors),
	}
}

// RecordSuccess notifies the collector of a successfully processed row.
func (c *ErrorCollector) RecordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalRows++
	c.consecutiveErrors = 0
}

// RecordError records a value-type row error and checks circuit breaker rules.
// Returns false if the circuit breaker tripped and streaming must halt.
func (c *ErrorCollector) RecordError(err stencil.RowError) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.circuitTripped {
		return false
	}

	c.totalErrors++
	c.totalRows++
	c.consecutiveErrors++

	if len(c.errors) < c.cfg.maxErrors {
		c.errors = append(c.errors, err)
	}

	// 1. Check consecutive error limit
	if c.cfg.consecutiveLimit > 0 && c.consecutiveErrors >= c.cfg.consecutiveLimit {
		c.circuitTripped = true
		c.tripReason = fmt.Sprintf("exceeded consecutive error limit (%d in a row)", c.consecutiveErrors)
		return false
	}

	// 2. Check error rate threshold
	if c.cfg.errorRateThreshold > 0 && c.totalRows >= int64(c.cfg.minRateSamples) {
		rate := float64(c.totalErrors) / float64(c.totalRows)
		if rate >= c.cfg.errorRateThreshold {
			c.circuitTripped = true
			c.tripReason = fmt.Sprintf("error rate %.1f%% exceeded threshold %.1f%% after %d rows",
				rate*100, c.cfg.errorRateThreshold*100, c.totalRows)
			return false
		}
	}

	// 3. Check absolute max errors limit
	if c.cfg.maxErrors > 0 && int(c.totalErrors) >= c.cfg.maxErrors && c.cfg.consecutiveLimit <= 0 && c.cfg.errorRateThreshold <= 0 {
		c.circuitTripped = true
		c.tripReason = fmt.Sprintf("exceeded maximum error capacity (%d errors)", c.cfg.maxErrors)
		return false
	}

	return true
}

// IsTripped returns whether the circuit breaker is currently open.
func (c *ErrorCollector) IsTripped() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.circuitTripped
}

// TripReason returns the explanation if the circuit breaker opened.
func (c *ErrorCollector) TripReason() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tripReason
}

// TotalErrors returns the total number of errors encountered.
func (c *ErrorCollector) TotalErrors() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalErrors
}

// TotalRows returns total rows processed.
func (c *ErrorCollector) TotalRows() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalRows
}

// Errors returns a copy of the stored bounded errors.
func (c *ErrorCollector) Errors() []stencil.RowError {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]stencil.RowError, len(c.errors))
	copy(out, c.errors)
	return out
}

// Reset clears the collector state for stream reuse without heap reallocation.
func (c *ErrorCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errors = c.errors[:0]
	c.totalErrors = 0
	c.totalRows = 0
	c.consecutiveErrors = 0
	c.circuitTripped = false
	c.tripReason = ""
}

// ProblemDetails formats the collected errors into an RFC 9457 structured ProblemDetails document.
func (c *ErrorCollector) ProblemDetails() stencil.ProblemDetails {
	c.mu.Lock()
	defer c.mu.Unlock()

	invalidParams := make([]stencil.InvalidParam, len(c.errors))
	for i, e := range c.errors {
		invalidParams[i] = stencil.InvalidParam{
			Row:      e.Row,
			Name:     e.Col,
			Reason:   e.Message,
			Code:     e.Code,
			Expected: e.Expected,
			Actual:   e.Actual,
		}
	}

	typ := "urn:stencil:error:validation"
	title := "Row Validation Failures"
	status := 422
	detail := fmt.Sprintf("Encountered %d row errors across %d rows processed", c.totalErrors, c.totalRows)

	if c.circuitTripped {
		typ = "urn:stencil:error:circuit-breaker"
		title = "Circuit Breaker Tripped"
		detail = fmt.Sprintf("Stream aborted: %s (total errors: %d, rows: %d)", c.tripReason, c.totalErrors, c.totalRows)
	}

	return stencil.ProblemDetails{
		Type:          typ,
		Title:         title,
		Status:        status,
		Detail:        detail,
		InvalidParams: invalidParams,
	}
}
