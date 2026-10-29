package errcontrol_test

import (
	"strings"
	"testing"

	"github.com/xdvi/stencil"
	"github.com/xdvi/stencil/errcontrol"
)

func TestErrorCollector_BoundedStorage(t *testing.T) {
	collector := errcontrol.NewCollector(errcontrol.WithMaxErrors(5), errcontrol.WithConsecutiveLimit(100))

	for i := 1; i <= 20; i++ {
		cont := collector.RecordError(stencil.RowError{
			Row:     i,
			Col:     "amount",
			Code:    "invalid_currency",
			Message: "Bad amount",
		})
		if !cont {
			t.Fatalf("expected streaming to continue at row %d", i)
		}
	}

	if collector.TotalErrors() != 20 {
		t.Errorf("expected 20 total errors, got %d", collector.TotalErrors())
	}
	if len(collector.Errors()) != 5 {
		t.Errorf("expected bounded storage of 5, got %d", len(collector.Errors()))
	}
	if collector.IsTripped() {
		t.Errorf("did not expect circuit breaker to trip")
	}
}

func TestErrorCollector_ConsecutiveCircuitBreaker(t *testing.T) {
	collector := errcontrol.NewCollector(errcontrol.WithConsecutiveLimit(5))

	for i := 1; i <= 4; i++ {
		if !collector.RecordError(stencil.RowError{Row: i, Code: "err"}) {
			t.Fatalf("row %d should not trip yet", i)
		}
	}

	// 5th consecutive error should trip the breaker
	if collector.RecordError(stencil.RowError{Row: 5, Code: "err"}) {
		t.Fatalf("row 5 should have tripped the circuit breaker")
	}

	if !collector.IsTripped() {
		t.Errorf("expected circuit breaker to be tripped")
	}
	if !strings.Contains(collector.TripReason(), "consecutive") {
		t.Errorf("expected trip reason to mention consecutive, got %q", collector.TripReason())
	}

	// Further errors rejected
	if collector.RecordError(stencil.RowError{Row: 6, Code: "err"}) {
		t.Errorf("expected subsequent RecordError to return false after tripping")
	}
}

func TestErrorCollector_SuccessResetsConsecutive(t *testing.T) {
	collector := errcontrol.NewCollector(errcontrol.WithConsecutiveLimit(3))

	collector.RecordError(stencil.RowError{Row: 1, Code: "err"})
	collector.RecordError(stencil.RowError{Row: 2, Code: "err"})

	// Success resets streak!
	collector.RecordSuccess()

	// Should not trip on 3rd error because streak was reset
	if !collector.RecordError(stencil.RowError{Row: 4, Code: "err"}) {
		t.Fatalf("streak was reset, should not trip")
	}
	if collector.IsTripped() {
		t.Errorf("breaker should not be tripped")
	}
}

func TestErrorCollector_ErrorRateCircuitBreaker(t *testing.T) {
	// Trip if error rate >= 40% after at least 10 samples
	collector := errcontrol.NewCollector(
		errcontrol.WithErrorRateThreshold(0.40, 10),
		errcontrol.WithConsecutiveLimit(100),
	)

	// First 6 rows: success
	for i := 0; i < 6; i++ {
		collector.RecordSuccess()
	}

	// Next 4 rows: errors (4 errors / 10 rows = 40% >= 40% threshold!)
	for i := 0; i < 3; i++ {
		if !collector.RecordError(stencil.RowError{Row: i + 7, Code: "err"}) {
			t.Fatalf("row %d should not trip yet", i+7)
		}
	}

	// 10th row (4th error -> 40% reached)
	if collector.RecordError(stencil.RowError{Row: 10, Code: "err"}) {
		t.Fatalf("expected circuit breaker to trip at row 10 with 40%% error rate")
	}

	if !collector.IsTripped() {
		t.Errorf("expected circuit breaker to be tripped")
	}
	if !strings.Contains(collector.TripReason(), "error rate") {
		t.Errorf("expected trip reason to mention error rate, got %q", collector.TripReason())
	}
}

func TestErrorCollector_ProblemDetails(t *testing.T) {
	collector := errcontrol.NewCollector(errcontrol.WithConsecutiveLimit(2))
	collector.RecordError(stencil.RowError{
		Row:     1,
		Col:     "id",
		Code:    "required_field",
		Message: "Missing required id",
	})
	collector.RecordError(stencil.RowError{
		Row:     2,
		Col:     "amount",
		Code:    "invalid_currency",
		Message: "Malformed amount",
	})

	pd := collector.ProblemDetails()
	if pd.Type != "urn:stencil:error:circuit-breaker" {
		t.Errorf("expected circuit-breaker type, got %s", pd.Type)
	}
	if pd.Status != 422 {
		t.Errorf("expected status 422, got %d", pd.Status)
	}
	if len(pd.InvalidParams) != 2 {
		t.Fatalf("expected 2 invalid params, got %d", len(pd.InvalidParams))
	}
	if pd.InvalidParams[0].Name != "id" || pd.InvalidParams[1].Name != "amount" {
		t.Errorf("invalid params mismatch: %+v", pd.InvalidParams)
	}
}

func BenchmarkErrorCollector_RecordSuccess(b *testing.B) {
	collector := errcontrol.NewCollector()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		collector.RecordSuccess()
	}
}

func BenchmarkErrorCollector_RecordError(b *testing.B) {
	collector := errcontrol.NewCollector(errcontrol.WithMaxErrors(100), errcontrol.WithConsecutiveLimit(1000000))
	rowErr := stencil.RowError{Row: 1, Col: "id", Code: "err", Message: "msg"}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if (i & 63) == 0 {
			collector.Reset()
		}
		collector.RecordError(rowErr)
	}
}
