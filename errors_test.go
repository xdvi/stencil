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

func TestRowError_ProblemDetailsMapping(t *testing.T) {
	tests := []struct {
		code   string
		typ    string
		title  string
		status int
		detail string
	}{
		{
			code:   "scanner_error",
			typ:    "urn:stencil:error:scanner",
			title:  "Malformed Input",
			status: 400,
			detail: "Malformed input at row 3 column price",
		},
		{
			code:   "stream_error",
			typ:    "urn:stencil:error:stream",
			title:  "Stream Failure",
			status: 500,
			detail: "Stream failure at row 3",
		},
		{
			code:   "canceled",
			typ:    "urn:stencil:error:canceled",
			title:  "Request Timeout",
			status: 408,
			detail: "Request canceled at row 3",
		},
		{
			code:   "invalid_currency",
			typ:    "urn:stencil:error:validation",
			title:  "Unprocessable Row Data",
			status: 422,
			detail: "Validation failed at row 3 column price",
		},
		{
			code:   "required_field",
			typ:    "urn:stencil:error:validation",
			title:  "Unprocessable Row Data",
			status: 422,
			detail: "Validation failed at row 3 column price",
		},
		{
			code:   "invalid_iso8601",
			typ:    "urn:stencil:error:validation",
			title:  "Unprocessable Row Data",
			status: 422,
			detail: "Validation failed at row 3 column price",
		},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			pd := stencil.RowError{
				Row:      3,
				Col:      "price",
				Code:     tt.code,
				Expected: "e",
				Actual:   "a",
				Message:  "boom",
			}.ProblemDetails()

			if pd.Type != tt.typ {
				t.Errorf("type: got %q, want %q", pd.Type, tt.typ)
			}
			if pd.Title != tt.title {
				t.Errorf("title: got %q, want %q", pd.Title, tt.title)
			}
			if pd.Status != tt.status {
				t.Errorf("status: got %d, want %d", pd.Status, tt.status)
			}
			if pd.Detail != tt.detail {
				t.Errorf("detail: got %q, want %q", pd.Detail, tt.detail)
			}

			if len(pd.InvalidParams) != 1 {
				t.Fatalf("expected 1 invalid param, got %d", len(pd.InvalidParams))
			}
			got := pd.InvalidParams[0]
			if got.Row != 3 || got.Name != "price" || got.Reason != "boom" || got.Code != tt.code ||
				got.Expected != "e" || got.Actual != "a" {
				t.Errorf("unexpected invalid param: %+v", got)
			}
		})
	}
}

func TestRowError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  stencil.RowError
		want string
	}{
		{
			name: "expected and actual",
			err:  stencil.RowError{Row: 1, Col: "c", Code: "x", Expected: "e", Actual: "a", Message: "msg"},
			want: "row 1, col c: msg (code=x, expected=e, actual=a)",
		},
		{
			name: "expected only",
			err:  stencil.RowError{Row: 1, Col: "c", Code: "x", Expected: "e", Message: "msg"},
			want: "row 1, col c: msg (code=x, expected=e)",
		},
		{
			name: "actual only",
			err:  stencil.RowError{Row: 1, Col: "c", Code: "x", Actual: "a", Message: "msg"},
			want: "row 1, col c: msg (code=x, actual=a)",
		},
		{
			name: "neither",
			err:  stencil.RowError{Row: 1, Col: "c", Code: "x", Message: "msg"},
			want: "row 1, col c: msg (code=x)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
