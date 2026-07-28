package imagefsd

import (
	"testing"
	"time"
)

func TestGCDaemons_CleansUpFailedDaemon(t *testing.T) {
	d := newTestDaemon("failed-1")
	d.mountFailed.Store(true)

	mgr := newTestManager(map[string]*Daemon{
		d.meta.ID: d,
	})

	mgr.gcDaemons()

	mgr.mu.RLock()
	_, exists := mgr.daemons[d.meta.ID]
	mgr.mu.RUnlock()

	if exists {
		t.Error("gcDaemons should have removed the failed daemon from the map")
	}
}

func TestGCDaemons_SkipsFailedDaemonWhenMountFailedCleared(t *testing.T) {
	d := newTestDaemon("recovered-1")
	d.mountFailed.Store(true)

	mgr := newTestManager(map[string]*Daemon{
		d.meta.ID: d,
	})

	d.isAliveFunc = func() bool {
		d.mountFailed.Store(false)
		return true
	}

	mgr.gcDaemons()

	mgr.mu.RLock()
	_, exists := mgr.daemons[d.meta.ID]
	mgr.mu.RUnlock()

	if !exists {
		t.Error("gcDaemons should NOT have removed the daemon because mountFailed was cleared")
	}
}

func TestGCDaemons_DoesNotAffectHealthyDaemons(t *testing.T) {
	healthy := newTestDaemon("healthy-1")
	healthy.mountFailed.Store(false)
	healthy.updateExpired()

	failed := newTestDaemon("failed-1")
	failed.mountFailed.Store(true)

	mgr := newTestManager(map[string]*Daemon{
		healthy.meta.ID: healthy,
		failed.meta.ID:  failed,
	})

	mgr.gcDaemons()

	mgr.mu.RLock()
	_, healthyExists := mgr.daemons[healthy.meta.ID]
	_, failedExists := mgr.daemons[failed.meta.ID]
	mgr.mu.RUnlock()

	if !healthyExists {
		t.Error("gcDaemons should NOT remove healthy daemon")
	}
	if failedExists {
		t.Error("gcDaemons should remove failed daemon")
	}
}

func TestGCDaemons_UnmountsAliveFailed(t *testing.T) {
	d := newTestDaemon("alive-failed")
	d.mountFailed.Store(true)
	d.isAliveFunc = func() bool { return true }
	d.stopChan = make(chan struct{})
	d.kickStop = NewStopper()

	go func() {
		<-d.kickStop.Done()
		close(d.stopChan)
	}()

	mgr := newTestManager(map[string]*Daemon{
		d.meta.ID: d,
	})

	mgr.gcDaemons()

	mgr.mu.RLock()
	_, exists := mgr.daemons[d.meta.ID]
	mgr.mu.RUnlock()

	if exists {
		t.Error("gcDaemons should have cleaned up the alive-but-failed daemon")
	}

	if d.getState() != DaemonStateStopped {
		t.Errorf("daemon state = %v, want Stopped after GC unmount", d.getState())
	}
}

func TestGCDaemons_ExpiryStillWorksWithFailedPass(t *testing.T) {
	expired := newTestDaemon("expired-1")
	expired.expiredAt = time.Now().Add(-1 * time.Hour).UnixNano()
	expired.isAliveFunc = func() bool { return false }

	failed := newTestDaemon("failed-1")
	failed.mountFailed.Store(true)

	healthy := newTestDaemon("healthy-1")
	healthy.updateExpired()
	healthy.isAliveFunc = func() bool { return true }

	mgr := newTestManager(map[string]*Daemon{
		expired.meta.ID: expired,
		failed.meta.ID:  failed,
		healthy.meta.ID: healthy,
	})

	mgr.root = t.TempDir()

	mgr.gcDaemons()

	mgr.mu.RLock()
	_, expiredExists := mgr.daemons[expired.meta.ID]
	_, failedExists := mgr.daemons[failed.meta.ID]
	_, healthyExists := mgr.daemons[healthy.meta.ID]
	mgr.mu.RUnlock()

	if expiredExists {
		t.Error("expired daemon should be cleaned by expiry-based GC")
	}
	if failedExists {
		t.Error("failed daemon should be cleaned by failed-daemon pass")
	}
	if !healthyExists {
		t.Error("healthy daemon should remain")
	}
}

func TestGCDaemons_NoDaemonsNoPanic(t *testing.T) {
	mgr := newTestManager(map[string]*Daemon{})
	mgr.gcDaemons()
}

func TestGCDaemons_AllHealthyNoDeletion(t *testing.T) {
	d1 := newTestDaemon("healthy-1")
	d1.updateExpired()
	d1.isAliveFunc = func() bool { return true }

	d2 := newTestDaemon("healthy-2")
	d2.updateExpired()
	d2.isAliveFunc = func() bool { return true }

	mgr := newTestManager(map[string]*Daemon{
		d1.meta.ID: d1,
		d2.meta.ID: d2,
	})

	mgr.gcDaemons()

	mgr.mu.RLock()
	count := len(mgr.daemons)
	mgr.mu.RUnlock()

	if count != 2 {
		t.Errorf("all healthy daemons should remain, got %d want 2", count)
	}
}

func TestGCDaemons_MultipleFailedDaemons(t *testing.T) {
	daemons := make(map[string]*Daemon)
	for i := 0; i < 5; i++ {
		id := t.TempDir()
		d := newTestDaemon(id)
		d.mountFailed.Store(true)
		daemons[id] = d
	}

	mgr := newTestManager(daemons)
	mgr.gcDaemons()

	mgr.mu.RLock()
	remaining := len(mgr.daemons)
	mgr.mu.RUnlock()

	if remaining != 0 {
		t.Errorf("all failed daemons should be cleaned, %d remain", remaining)
	}
}
