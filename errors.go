package stencil

import "fmt"

// RowError represents an issue encountered while parsing or validating a row.
type RowError struct {
	Row      int    `json:"row"`
	Col      string `json:"col"`
	Code     string `json:"code"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
	Message  string `json:"message"`
}

func (e RowError) Error() string {
	return fmt.Sprintf("row %d, col %s: %s (code=%s, expected=%s, actual=%s)",
		e.Row, e.Col, e.Message, e.Code, e.Expected, e.Actual)
}

// ProblemDetails represents an RFC 9457 structured error document.
type ProblemDetails struct {
	Type          string         `json:"type"`
	Title         string         `json:"title"`
	Status        int            `json:"status"`
	Detail        string         `json:"detail"`
	Instance      string         `json:"instance,omitempty"`
	InvalidParams []InvalidParam `json:"invalid_params,omitempty"`
}

// InvalidParam details a single invalid parameter or column.
type InvalidParam struct {
	Row      int    `json:"row"`
	Name     string `json:"name"`
	Reason   string `json:"reason"`
	Code     string `json:"code"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

// ProblemDetails converts a RowError into an RFC 9457 format.
func (e RowError) ProblemDetails() ProblemDetails {
	return ProblemDetails{
		Type:   "urn:stencil:error:validation",
		Title:  "Unprocessable Row Data",
		Status: 422,
		Detail: fmt.Sprintf("Validation failed at row %d column %s", e.Row, e.Col),
		InvalidParams: []InvalidParam{
			{
				Row:      e.Row,
				Name:     e.Col,
				Reason:   e.Message,
				Code:     e.Code,
				Expected: e.Expected,
				Actual:   e.Actual,
			},
		},
	}
}
