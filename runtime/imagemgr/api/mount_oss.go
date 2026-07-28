package api

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
)

func (w *HttpWorker) MountOSS(ctx context.Context, req *OSSMountRequest) (*MountInfo, error) {
	if err := validateLeaseID(req.LeaseID); err != nil {
		return nil, err
	}
	if err := validateOwner(req.Owner); err != nil {
		return nil, err
	}
	if w.mountStore == nil {
		return nil, fmt.Errorf("mount store is not initialized")
	}
	key := "oss:" + generateOSSID(req.Endpoint, req.Bucket, req.Object)
	unlock := w.lockMount(key)
	defer unlock()
	if lease, _, err := w.existingLease(req.LeaseID); err != nil {
		return nil, err
	} else if lease != nil && lease.MountKey != key {
		return nil, fmt.Errorf("lease %q already owns mount %q", req.LeaseID, lease.MountKey)
	}
	record, err := w.mountStore.GetMount(key)
	if err != nil {
		return nil, err
	}
	info, err := w.ensureOSSMounted(ctx, req)
	if err != nil {
		return nil, err
	}
	createdResource := record == nil
	if record == nil {
		record = &mountstore.Record{CacheKey: key, MountType: string(MountTypeOSS), MountPoint: info.MountPath, Endpoint: req.Endpoint, Bucket: req.Bucket, Object: req.Object}
	}
	if _, err := w.mountStore.Acquire(record, req.LeaseID, req.Owner); err != nil {
		if createdResource {
			_ = w.unmountOSSResource(ctx, record)
		}
		return nil, fmt.Errorf("persist OSS mount lease: %w", err)
	}
	return info, nil
}

func (w *HttpWorker) ensureOSSMounted(ctx context.Context, req *OSSMountRequest) (*MountInfo, error) {
	// Generate daemon ID first for tracing
	daemonID := generateOSSID(req.Endpoint, req.Bucket, req.Object)

	// Start API timing
	timing, _ := StartAPITimedOperation(ctx, "api.MountOSS", daemonID)
	defer timing.End()

	// Stage 1: Parse and validate request
	stageStart := time.Now()
	prefix, name, err := splitObject(req.Object)
	if err != nil {
		timing.Fail(err)
		return nil, fmt.Errorf("invalid object: %w", err)
	}
	timing.Stage("parse_request", time.Since(stageStart))

	stageStart = time.Now()
	if w.ossLoopMgr == nil {
		err := fmt.Errorf("oss loop manager is not initialized")
		timing.Fail(err)
		return nil, err
	}
	timing.Stage("validate_dependencies", time.Since(stageStart))

	// Stage 2: Create daemon options
	stageStart = time.Now()
	opts := &imagefsd.DaemonCreateOpt{
		ID:              daemonID,
		Name:            name,
		MountPoint:      req.MountPoint,
		ObjectPrefix:    prefix,
		Endpoint:        req.Endpoint,
		Bucket:          req.Bucket,
		AccessKeyID:     req.AccessKeyID,
		AccessKeySecret: req.AccessKeySecret,
	}
	logrus.Infof("%s %s %s has ID %s", req.Endpoint, req.Bucket, req.Object, opts.ID)
	logrus.WithFields(logrus.Fields{
		"daemon_id":   opts.ID,
		"mount_point": req.MountPoint,
		"source_type": imagefsd.SourceTypeOSS,
		"endpoint":    req.Endpoint,
		"bucket":      req.Bucket,
		"object":      req.Object,
	}).Info("mount path daemon info")
	timing.Stage("prepare_options", time.Since(stageStart))

	// Stage 3: Create daemon
	stageStart = time.Now()
	daemonExisted := w.mgr.GetDaemon(opts.ID) != nil
	err = w.mgr.CreateDaemon(opts)
	if err != nil {
		timing.Fail(err)
		return nil, fmt.Errorf("failed to create daemon: %w", err)
	}
	logrus.Infof("daemon %s is ready to mount", opts.ID)
	timing.Stage("create_daemon", time.Since(stageStart))

	// Stage 4: Get daemon
	stageStart = time.Now()
	d := w.mgr.GetDaemon(opts.ID)
	if d == nil {
		err := fmt.Errorf("can't find daemon, id = %s", opts.ID)
		timing.Fail(err)
		return nil, err
	}
	rawImagePath := filepath.Join(d.MountPoint(), d.Name())
	timing.Stage("get_daemon", time.Since(stageStart))

	// Stage 5: Mount daemon (this will have its own detailed timing)
	stageStart = time.Now()
	logrus.WithFields(logrus.Fields{
		"daemon_id":   opts.ID,
		"mount_point": d.MountPoint(),
		"source_type": imagefsd.SourceTypeOSS,
	}).Info("mount path begin daemon mount")
	if err = d.Mount(); err != nil {
		timing.Fail(err)
		return nil, fmt.Errorf("failed to mount %s: %w", opts.ID, err)
	}
	timing.Stage("daemon_mount", time.Since(stageStart))

	// Stage 6: loop-mount the ext4 image as a directory rootfs
	stageStart = time.Now()
	rootfsPath, err := w.ossLoopMgr.EnsureMounted(opts.ID, rawImagePath)
	if err != nil {
		if !daemonExisted {
			_ = d.Unmount()
		}
		timing.Fail(err)
		return nil, fmt.Errorf("failed to expose mounted oss object as rootfs directory: %w", err)
	}
	info := &MountInfo{
		MountPath:  rootfsPath,
		MountPoint: d.MountPoint(),
	}
	logrus.WithFields(logrus.Fields{
		"daemon_id":         opts.ID,
		"mount_point":       d.MountPoint(),
		"raw_image_path":    rawImagePath,
		"rootfs_mount_path": info.MountPath,
		"source_type":       imagefsd.SourceTypeOSS,
	}).Info("mount path daemon mount completed")
	timing.Stage("loop_mount", time.Since(stageStart))

	return info, nil
}

func (w *HttpWorker) UnmountOSS(ctx context.Context, req *OSSUmountRequest) (*MountInfo, error) {
	err := w.releaseLease(req.LeaseID, func(record *mountstore.Record) error { return w.unmountOSSResource(ctx, record) })
	if err != nil {
		return nil, err
	}
	return &MountInfo{}, nil
}

func (w *HttpWorker) unmountOSSResource(ctx context.Context, record *mountstore.Record) error {
	// Generate daemon ID
	id := generateOSSID(record.Endpoint, record.Bucket, record.Object)

	// Start API timing
	timing, _ := StartAPITimedOperation(ctx, "api.UnmountOSS", id)
	defer timing.End()

	// Stage 1: Get daemon
	stageStart := time.Now()
	if w.ossLoopMgr == nil {
		err := fmt.Errorf("oss loop manager is not initialized")
		timing.Fail(err)
		return err
	}
	d := w.mgr.GetDaemon(id)
	timing.Stage("get_daemon", time.Since(stageStart))

	// Stage 2: Unmount loop-mounted rootfs directory
	stageStart = time.Now()
	unmountResult, err := w.ossLoopMgr.ReleaseResource(id)
	if err != nil {
		timing.Fail(err)
		return fmt.Errorf("failed to unmount oss rootfs %s: %w", id, err)
	}
	timing.Stage("loop_unmount", time.Since(stageStart))

	if !unmountResult.Released {
		return fmt.Errorf("oss rootfs %s retained an unexpected internal reference", id)
	}

	// Stage 3: Unmount daemon (this will have its own detailed timing)
	if d == nil {
		return nil
	}
	stageStart = time.Now()
	logrus.WithFields(logrus.Fields{
		"daemon_id":   id,
		"mount_point": d.MountPoint(),
		"source_type": imagefsd.SourceTypeOSS,
	}).Info("unmount path begin daemon unmount")
	err = d.Unmount()
	if err != nil {
		timing.Fail(err)
		return fmt.Errorf("failed to umount daemon %s: %w", id, err)
	}
	logrus.WithFields(logrus.Fields{
		"daemon_id":   id,
		"mount_point": d.MountPoint(),
		"source_type": imagefsd.SourceTypeOSS,
	}).Info("unmount path daemon unmount completed")
	timing.Stage("daemon_unmount", time.Since(stageStart))

	return nil
}
