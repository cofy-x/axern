package validate

import (
	"fmt"

	"github.com/cofy-x/axern/apps/axrun/internal/schema"
)

type Params struct {
	RunDir string
}

type Result struct {
	RunID    string
	RunDir   string
	Problems []schema.Problem
}

func (r Result) Valid() bool {
	return len(r.Problems) == 0
}

type Error struct {
	Result Result
}

func (e Error) Error() string {
	if len(e.Result.Problems) == 0 {
		return "Axrun run validation failed"
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
		return "Axrun run validation failed: " + detail
	}
	return fmt.Sprintf("Axrun run validation failed with %d problem(s): %s", len(e.Result.Problems), detail)
}
