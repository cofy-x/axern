package workspace

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWorkspaceLockRetainsAllWaitersUntilTheLastRelease(t *testing.T) {
	m := New(Options{})
	unlockFirst := m.lockWorkspace("same")
	var firstOnce sync.Once
	releaseFirst := func() { firstOnce.Do(unlockFirst) }
	t.Cleanup(releaseFirst)
	m.locksMu.Lock()
	original := m.locks["same"]
	m.locksMu.Unlock()
	secondEntered, secondDone := make(chan struct{}), make(chan struct{})
	releaseSecond := make(chan struct{})
	var secondOnce sync.Once
	finishSecond := func() { secondOnce.Do(func() { close(releaseSecond) }) }
	t.Cleanup(finishSecond)
	go func() {
		unlock := m.lockWorkspace("same")
		close(secondEntered)
		<-releaseSecond
		unlock()
		close(secondDone)
	}()
	waitForRefs := func(want int) {
		t.Helper()
		require.Eventually(t, func() bool {
			m.locksMu.Lock()
			defer m.locksMu.Unlock()
			return m.locks["same"] == original && original.refs == want
		}, time.Second, time.Millisecond, "holders and registered waiters must share one live entry")
	}
	waitForRefs(2)
	releaseFirst()
	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("registered second owner did not enter")
	}
	waitForRefs(1)
	thirdEntered, thirdDone := make(chan struct{}), make(chan struct{})
	go func() {
		unlock := m.lockWorkspace("same")
		close(thirdEntered)
		unlock()
		close(thirdDone)
	}()
	waitForRefs(2)
	select {
	case <-thirdEntered:
		t.Fatal("a fresh caller bypassed the waiting owner's mutex")
	default:
	}
	unlockOther := m.lockWorkspace("unrelated")
	unlockOther()
	finishSecond()
	for _, done := range []<-chan struct{}{secondDone, thirdDone} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Workspace lock caller did not finish")
		}
	}
	m.locksMu.Lock()
	defer m.locksMu.Unlock()
	require.Empty(t, m.locks, "no holder or waiter remains, so the in-memory entry can retire")
}

func TestWorkspaceLockChurnDoesNotRetainRetiredIDs(t *testing.T) {
	m := New(Options{})
	for index := range 4096 {
		unlock := m.lockWorkspace(fmt.Sprintf("workspace-%d", index))
		unlock()
	}
	m.locksMu.Lock()
	defer m.locksMu.Unlock()
	require.Empty(t, m.locks)
}
