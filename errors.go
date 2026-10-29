package stencil

import (
	"fmt"
	"strings"
)

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
	var b strings.Builder
	fmt.Fprintf(&b, "row %d, col %s: %s (code=%s", e.Row, e.Col, e.Message, e.Code)
	if e.Expected != "" {
		fmt.Fprintf(&b, ", expected=%s", e.Expected)
	}
	if e.Actual != "" {
		fmt.Fprintf(&b, ", actual=%s", e.Actual)
	}
	b.WriteString(")")
	return b.String()
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

// problemType classifies a row error code into an RFC 9457 problem document.
func (e RowError) problemType() (typ, title string, status int, detail string) {
	switch e.Code {
	case "scanner_error":
		return "urn:stencil:error:scanner", "Malformed Input", 400,
			fmt.Sprintf("Malformed input at row %d column %s", e.Row, e.Col)
	case "stream_error":
		return "urn:stencil:error:stream", "Stream Failure", 500,
			fmt.Sprintf("Stream failure at row %d", e.Row)
	case "canceled":
		return "urn:stencil:error:canceled", "Request Timeout", 408,
			fmt.Sprintf("Request canceled at row %d", e.Row)
	default:
		return "urn:stencil:error:validation", "Unprocessable Row Data", 422,
			fmt.Sprintf("Validation failed at row %d column %s", e.Row, e.Col)
	}
}

// ProblemDetails converts a RowError into an RFC 9457 format.
func (e RowError) ProblemDetails() ProblemDetails {
	typ, title, status, detail := e.problemType()
	return ProblemDetails{
		Type:   typ,
		Title:  title,
		Status: status,
		Detail: detail,
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
