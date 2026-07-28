package schema

import (
	"fmt"
	"strings"
)

type Severity string

const (
	SeverityError Severity = "error"
)

type Problem struct {
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	Field    string   `json:"field,omitempty"`
	Message  string   `json:"message"`
}

type Result struct {
	RunID    string
	RunDir   string
	Problems []Problem
}

func (r Result) Valid() bool {
	return len(r.Problems) == 0
}

type ValidationError struct {
	Result Result
}

func (e ValidationError) Error() string {
	if len(e.Result.Problems) == 0 {
		return "schema validation failed"
	}
	first := e.Result.Problems[0]
	detail := first.Message
	if first.Field != "" {
		detail = first.Field + ": " + detail
	}
	if first.Path != "" {
		detail = first.Path + ": " + detail
	}
	if len(e.Result.Problems) == 1 {
		return "schema validation failed: " + detail
	}
	return fmt.Sprintf("schema validation failed with %d problem(s): %s", len(e.Result.Problems), detail)
}

type collector struct {
	problems []Problem
}

func (c *collector) add(path string, field string, message string) {
	c.problems = append(c.problems, Problem{
		Severity: SeverityError,
		Path:     path,
		Field:    field,
		Message:  message,
	})
}

func (c *collector) required(path string, field string, value string) {
	if strings.TrimSpace(value) == "" {
		c.add(path, field, "is required")
	}
}

func (c *collector) empty(path string, field string, value string) {
	if strings.TrimSpace(value) != "" {
		c.add(path, field, "must be empty")
	}
}

func (c *collector) requiredInt(path string, field string, value int) {
	if value <= 0 {
		c.add(path, field, "must be greater than zero")
	}
}
