package environmentcache

import (
	"fmt"

	runtime_api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/sirupsen/logrus"
)

func (rf *RootFS) IncActiveRef() error {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.released {
		return fmt.Errorf("rootfs references have already been released")
	}
	rf.activeRefs++
	return nil
}

func (rf *RootFS) MoveActiveToRetained() error {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.released {
		return fmt.Errorf("cannot retain released rootfs %v", rf.cfg)
	}
	if rf.activeRefs == 0 {
		return fmt.Errorf("rootfs %v has no active reference to retain", rf.cfg)
	}
	rf.activeRefs--
	rf.retainedRefs++
	return nil
}

func (rf *RootFS) MoveRetainedToActive() error {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.released {
		return fmt.Errorf("rootfs references have already been released")
	}
	if rf.retainedRefs <= 0 {
		return fmt.Errorf("rootfs %v has no retained references", rf.cfg)
	}
	rf.retainedRefs--
	rf.activeRefs++
	return nil
}

// ReleaseActiveRef reports whether the final reference was relinquished. A true
// result does not imply physical unmount succeeded; the error reports cleanup.
func (rf *RootFS) ReleaseActiveRef() (bool, error) {
	return rf.releaseRef(true)
}

func (rf *RootFS) ReleaseRetainedRef() (bool, error) {
	return rf.releaseRef(false)
}

func (rf *RootFS) RetainedRefCount() int64 {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.retainedRefs
}

func (rf *RootFS) RootfsTypeLabel() string {
	switch rf.cfg.SrcType {
	case runtime_api.RootfsSrcType_LOCAL:
		return "local"
	case runtime_api.RootfsSrcType_IMAGE:
		return "image"
	default:
		return "unknown"
	}
}

func (rf *RootFS) referencesReleased() bool {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.released
}

func (rf *RootFS) releaseRef(active bool) (bool, error) {
	rf.mu.Lock()
	if rf.released {
		rf.mu.Unlock()
		return false, nil
	}

	if active {
		if rf.activeRefs == 0 {
			rf.mu.Unlock()
			return false, fmt.Errorf("rootfs %v has no active reference to release", rf.cfg)
		}
		rf.activeRefs--
	} else {
		if rf.retainedRefs == 0 {
			rf.mu.Unlock()
			return false, fmt.Errorf("rootfs %v has no retained reference to release", rf.cfg)
		}
		rf.retainedRefs--
	}

	shouldRelease := rf.activeRefs == 0 && rf.retainedRefs == 0
	if shouldRelease {
		logrus.Infof("No one refers rootfs %v, try to release it", rf.cfg)
		rf.released = true
	}
	rf.mu.Unlock()

	if !shouldRelease {
		return false, nil
	}

	if rf.releaseMount != nil {
		return true, rf.releaseMount()
	}
	return true, rf.UmountImage()
}
