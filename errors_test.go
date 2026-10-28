package stencil_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xdvi/stencil"
)

func TestRowError_ProblemDetails(t *testing.T) {
	rowErr := stencil.RowError{
		Row:      42,
		Col:      "birth_date",
		Code:     "invalid_iso8601",
		Expected: "YYYY-MM-DDTHH:MM:SSZ",
		Actual:   "31/02/2026",
		Message:  "Timestamp does not conform to ISO 8601",
	}

	pd := rowErr.ProblemDetails()
	if pd.Status != 422 {
		t.Fatalf("expected status 422, got %d", pd.Status)
	}
	if pd.Type != "urn:stencil:error:validation" {
		t.Fatalf("expected urn:stencil:error:validation, got %s", pd.Type)
	}

	bytes, err := json.Marshal(pd)
	if err != nil {
		t.Fatalf("failed to marshal problem details: %v", err)
	}

	raw := string(bytes)
	if !strings.Contains(raw, `"invalid_params"`) || !strings.Contains(raw, `"row":42`) {
		t.Fatalf("expected invalid_params and row 42, got %s", raw)
	}
}
