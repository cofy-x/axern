package runtime

import (
	"context"
	"time"
)

func (r *RunscServiceHandler) waitRetryContext(parent context.Context) (context.Context, context.CancelFunc) {
	if runscWaitRetryTimeout <= 0 {
		return parent, func() {}
	}
	if deadline, ok := parent.Deadline(); ok && time.Until(deadline) <= runscWaitRetryTimeout {
		return parent, func() {}
	}
	return context.WithTimeout(parent, runscWaitRetryTimeout)
}
