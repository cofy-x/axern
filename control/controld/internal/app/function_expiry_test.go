package app

import (
	"context"
	"testing"
)

func TestExpireAsyncInvocationBatchesDrainsBacklog(t *testing.T) {
	store := &fakeFunctionInvocationExpirer{remaining: 250}
	if err := expireAsyncInvocationBatches(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	if store.remaining != 0 || store.calls != 3 {
		t.Fatalf("remaining=%d calls=%d, want fully drained in three batches", store.remaining, store.calls)
	}
}

type fakeFunctionInvocationExpirer struct {
	remaining int
	calls     int
}

func (f *fakeFunctionInvocationExpirer) ExpireAsyncInvocations(_ context.Context, limit int) (int, error) {
	f.calls++
	if f.remaining < limit {
		expired := f.remaining
		f.remaining = 0
		return expired, nil
	}
	f.remaining -= limit
	return limit, nil
}
