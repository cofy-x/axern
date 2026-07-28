package langruntime

import (
	"fmt"

	runtime_api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/sirupsen/logrus"
)

func (rf *RootFS) IncActiveRef() error {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.deleted {
		return fmt.Errorf("this rootfs has already been deleted")
	}
	rf.activeRefs++
	return nil
}

func (rf *RootFS) MoveActiveToRetained() {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.deleted {
		logrus.Warningf("attempt to retain deleted rootfs %v", rf.cfg)
		return
	}
	rf.activeRefs--
	if rf.activeRefs < 0 {
		logrus.Warningf("active rootfs refcount %v < 0, leak happens.", rf.activeRefs)
		rf.activeRefs = 0
		return
	}
	rf.retainedRefs++
}

func (rf *RootFS) MoveRetainedToActive() error {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	if rf.deleted {
		return fmt.Errorf("this rootfs has already been deleted")
	}
	if rf.retainedRefs <= 0 {
		return fmt.Errorf("rootfs %v has no retained references", rf.cfg)
	}
	rf.retainedRefs--
	rf.activeRefs++
	return nil
}

func (rf *RootFS) ReleaseActiveRef() bool {
	return rf.releaseRef(true)
}

func (rf *RootFS) ReleaseRetainedRef() bool {
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
	case runtime_api.RootfsSrcType_S3:
		return "s3"
	default:
		return "unknown"
	}
}

func (rf *RootFS) releaseRef(active bool) bool {
	rf.mu.Lock()
	if rf.deleted {
		rf.mu.Unlock()
		return false
	}

	if active {
		rf.activeRefs--
		if rf.activeRefs < 0 {
			logrus.Warningf("active rootfs refcount %v < 0, leak happens.", rf.activeRefs)
			rf.activeRefs = 0
			rf.mu.Unlock()
			return false
		}
	} else {
		rf.retainedRefs--
		if rf.retainedRefs < 0 {
			logrus.Warningf("retained rootfs refcount %v < 0, leak happens.", rf.retainedRefs)
			rf.retainedRefs = 0
			rf.mu.Unlock()
			return false
		}
	}

	shouldRelease := rf.activeRefs == 0 && rf.retainedRefs == 0
	if shouldRelease {
		logrus.Infof("No one refers rootfs %v, try to release it", rf.cfg)
		rf.deleted = true
	}
	rf.mu.Unlock()

	if !shouldRelease {
		return false
	}

	if err := rf.UmountImage(); err != nil {
		logrus.WithError(err).WithField("rootfs_path", rf.path).Warn("direct rootfs release failed; desired lease reconciliation will retry cleanup")
	}
	if rf.cleanupFunc != nil {
		rf.cleanupFunc()
	}
	return true
}
