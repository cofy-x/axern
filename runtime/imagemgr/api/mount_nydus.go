package api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/sirupsen/logrus"

	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"github.com/cofy-x/axern/runtime/imagemgr/pkg/registryauth"
)

func (w *HttpWorker) MountNydus(ctx context.Context, req *NydusMountRequest) (*MountInfo, error) {
	if err := validateLeaseID(req.LeaseID); err != nil {
		return nil, err
	}
	if err := validateOwner(req.Owner); err != nil {
		return nil, err
	}
	if w.mountStore == nil {
		return nil, fmt.Errorf("mount store is not initialized")
	}
	key := "nydus:" + req.ImageURL
	unlock := w.lockMount(key)
	defer unlock()
	if lease, record, err := w.existingLease(req.LeaseID); err != nil {
		return nil, err
	} else if lease != nil && lease.MountKey != key {
		return nil, fmt.Errorf("lease %q already owns mount %q", req.LeaseID, lease.MountKey)
	} else if record != nil {
		info, err := w.ensureNydusMounted(ctx, req)
		if err != nil {
			return nil, err
		}
		if err := attachNydusImmutableMount(info, req); err != nil {
			return nil, err
		}
		if _, err := w.mountStore.Acquire(record, req.LeaseID, req.Owner); err != nil {
			return nil, fmt.Errorf("persist Nydus mount lease: %w", err)
		}
		return info, nil
	}
	info, err := w.ensureNydusMounted(ctx, req)
	if err != nil {
		return nil, err
	}
	record := &mountstore.Record{CacheKey: key, ImageURL: req.ImageURL, MountType: string(MountTypeNydus), NydusImageURL: req.ImageURL, MountPoint: info.MountPath}
	if err := attachNydusImmutableMount(info, req); err != nil {
		_ = w.unmountNydusResource(ctx, req.ImageURL)
		return nil, err
	}
	if _, err := w.mountStore.Acquire(record, req.LeaseID, req.Owner); err != nil {
		if rollbackErr := w.unmountNydusResource(ctx, req.ImageURL); rollbackErr != nil {
			return nil, fmt.Errorf("persist Nydus mount lease: %w; rollback mount: %v", err, rollbackErr)
		}
		return nil, fmt.Errorf("persist Nydus mount lease: %w", err)
	}
	return info, nil
}

func attachNydusImmutableMount(info *MountInfo, req *NydusMountRequest) error {
	if info == nil || req == nil {
		return fmt.Errorf("Nydus immutable mount requires mount info and request")
	}
	descriptor, err := immutableMountDescriptor(info.MountPath, req.LeaseID, "nydus", generateNydusID(req.ImageURL), []string{info.MountPath}, []string{"nydus"})
	if err != nil {
		return fmt.Errorf("describe Nydus immutable mount: %w", err)
	}
	info.ImmutableMount = descriptor
	return nil
}

func (w *HttpWorker) ensureNydusMounted(ctx context.Context, req *NydusMountRequest) (*MountInfo, error) {
	// Generate daemon ID
	id := generateNydusID(req.ImageURL)

	// Start API timing
	timing, _ := StartAPITimedOperation(ctx, "api.MountNydus", id)
	defer timing.End()

	// Stage 1: Validate
	stageStart := time.Now()
	if w.nydusClient == nil {
		err := fmt.Errorf("nydus client is not initialized")
		timing.Fail(err)
		return nil, err
	}
	if req.ImageURL == "" {
		err := fmt.Errorf("image_url is required")
		timing.Fail(err)
		return nil, err
	}
	timing.Stage("validate_request", time.Since(stageStart))

	// Stage 2: Check if daemon already exists
	stageStart = time.Now()
	d := w.mgr.GetDaemon(id)
	if d != nil {
		logrus.Infof("daemon %s for image %s already exists, reusing it", id, req.ImageURL)

		info := &MountInfo{
			MountPath:  d.MountPoint(),
			MountPoint: d.MountPoint(),
			Env:        d.Env(),
		}
		timing.Stage("check_existing_daemon", time.Since(stageStart))

		// Mount if not already mounted
		stageStart = time.Now()
		if err := d.Mount(); err != nil {
			timing.Fail(err)
			return nil, fmt.Errorf("failed to mount existing daemon %s: %w", id, err)
		}
		timing.Stage("mount_existing_daemon", time.Since(stageStart))

		return info, nil
	}
	timing.Stage("check_existing_daemon", time.Since(stageStart))

	// Stage 3: Create daemon options
	stageStart = time.Now()
	opts := &imagefsd.DaemonCreateOpt{
		ID:               id,
		Name:             strings.ReplaceAll(req.ImageURL, "/", "_"),
		MountPoint:       req.MountPoint,
		SourceType:       imagefsd.SourceTypeNydus,
		ImageURL:         req.ImageURL,
		DockerConfigJSON: req.DockerConfigJSON,
	}
	if req.DockerConfigJSON != "" {
		auths, err := registryauth.Parse([]byte(req.DockerConfigJSON))
		if err != nil {
			return nil, fmt.Errorf("parse Nydus registry auth: %w", err)
		}
		ref, err := name.ParseReference(req.ImageURL)
		if err != nil {
			return nil, fmt.Errorf("parse Nydus image reference: %w", err)
		}
		opts.RegistryAuth = auths.Resolve(ref.Context().RegistryStr(), ref.Context().RepositoryStr())
	}
	logrus.WithFields(logrus.Fields{
		"daemon_id":   opts.ID,
		"mount_point": req.MountPoint,
		"source_type": imagefsd.SourceTypeNydus,
		"image_url":   req.ImageURL,
	}).Info("mount path daemon info")
	timing.Stage("prepare_options", time.Since(stageStart))

	// Stage 4: Create daemon (bootstrap download happens here)
	stageStart = time.Now()
	if err := w.mgr.CreateDaemon(opts); err != nil {
		timing.Fail(err)
		return nil, fmt.Errorf("failed to create daemon: %w", err)
	}
	logrus.Infof("daemon %s is ready to mount", opts.ID)
	timing.Stage("create_daemon", time.Since(stageStart))

	// Stage 5: Get daemon
	stageStart = time.Now()
	d = w.mgr.GetDaemon(opts.ID)
	if d == nil {
		err := fmt.Errorf("can't find daemon, id = %s", opts.ID)
		timing.Fail(err)
		return nil, err
	}

	info := &MountInfo{
		MountPath:  d.MountPoint(),
		MountPoint: d.MountPoint(),
	}
	timing.Stage("get_daemon", time.Since(stageStart))

	// Stage 6: Mount daemon
	stageStart = time.Now()
	logrus.WithFields(logrus.Fields{
		"daemon_id":   opts.ID,
		"mount_point": d.MountPoint(),
		"source_type": imagefsd.SourceTypeNydus,
		"image_url":   req.ImageURL,
	}).Info("mount path begin daemon mount")
	if err := d.Mount(); err != nil {
		timing.Fail(err)
		return nil, fmt.Errorf("failed to mount %s: %w", opts.ID, err)
	}
	info.Env = d.Env()
	logrus.WithFields(logrus.Fields{
		"daemon_id":   opts.ID,
		"mount_point": d.MountPoint(),
		"source_type": imagefsd.SourceTypeNydus,
		"image_url":   req.ImageURL,
	}).Info("mount path daemon mount completed")
	timing.Stage("daemon_mount", time.Since(stageStart))

	return info, nil
}

func (w *HttpWorker) UnmountNydus(ctx context.Context, req *NydusUmountRequest) (*MountInfo, error) {
	err := w.releaseLease(req.LeaseID, func(record *mountstore.Record) error {
		return w.unmountNydusResource(ctx, record.NydusImageURL)
	})
	if err != nil {
		return nil, err
	}
	return &MountInfo{}, nil
}

func (w *HttpWorker) unmountNydusResource(ctx context.Context, imageURL string) error {
	// Generate daemon ID
	id := generateNydusID(imageURL)

	// Start API timing
	timing, _ := StartAPITimedOperation(ctx, "api.UnmountNydus", id)
	defer timing.End()

	// Stage 1: Validate
	stageStart := time.Now()
	if imageURL == "" {
		err := fmt.Errorf("image_url is required")
		timing.Fail(err)
		return err
	}
	timing.Stage("validate_request", time.Since(stageStart))

	// Stage 2: Get daemon
	stageStart = time.Now()
	d := w.mgr.GetDaemon(id)
	if d == nil {
		return nil
	}
	timing.Stage("get_daemon", time.Since(stageStart))

	// Stage 3: Unmount daemon
	stageStart = time.Now()
	logrus.WithFields(logrus.Fields{
		"daemon_id":   id,
		"mount_point": d.MountPoint(),
		"source_type": imagefsd.SourceTypeNydus,
		"image_url":   imageURL,
	}).Info("unmount path begin daemon unmount")
	err := d.Unmount()
	if err != nil {
		timing.Fail(err)
		return fmt.Errorf("failed to umount daemon %s: %w", id, err)
	}
	logrus.WithFields(logrus.Fields{
		"daemon_id":   id,
		"mount_point": d.MountPoint(),
		"source_type": imagefsd.SourceTypeNydus,
		"image_url":   imageURL,
	}).Info("unmount path daemon unmount completed")
	timing.Stage("daemon_unmount", time.Since(stageStart))

	return nil
}

type nydusMountAttempt struct {
	mountPoint string
	env        []string
	detected   bool
}

// tryMountNydus attempts to mount an image as Nydus format.
// Returns mount point on success, or error if image is not Nydus or mount fails.
func (w *HttpWorker) tryMountNydus(ctx context.Context, imageURL, dockerConfigJSON string) (nydusMountAttempt, error) {
	isNydus := false
	switch cached := w.nydusCache.lookup(imageURL); cached {
	case nydusCacheOCI:
		logrus.Debugf("cache hit: %s is not a Nydus image", imageURL)
		return nydusMountAttempt{}, fmt.Errorf("not a Nydus image (cached)")
	case nydusCacheNydus:
		isNydus = true
	default:
		var err error
		isNydus, err = w.detectNydusOnce(ctx, nydusDetectionKey(imageURL, dockerConfigJSON), func(sharedCtx context.Context) (bool, error) {
			switch cached := w.nydusCache.lookup(imageURL); cached {
			case nydusCacheOCI:
				return false, nil
			case nydusCacheNydus:
				return true, nil
			}

			logrus.Debugf("cache miss for %s, fetching from registry", imageURL)
			detected, err := w.nydusClient.DetectImageWithDockerConfigJSON(sharedCtx, imageURL, w.registryProxyURL, dockerConfigJSON)
			if err != nil {
				return false, fmt.Errorf("failed to check image format: %w", err)
			}
			w.nydusCache.set(imageURL, detected)
			return detected, nil
		})
		if err != nil {
			return nydusMountAttempt{}, err
		}
	}

	if !isNydus {
		return nydusMountAttempt{}, fmt.Errorf("not a Nydus image")
	}
	if err := ctx.Err(); err != nil {
		return nydusMountAttempt{detected: true}, err
	}

	logrus.Infof("detected Nydus image at %s, routing to Nydus mount", imageURL)
	info, err := w.ensureNydusMounted(ctx, &NydusMountRequest{ImageURL: imageURL, DockerConfigJSON: dockerConfigJSON})
	if err != nil {
		return nydusMountAttempt{detected: true}, fmt.Errorf("failed to mount Nydus image: %w", err)
	}
	return nydusMountAttempt{mountPoint: info.MountPath, env: info.Env, detected: true}, nil
}

func nydusDetectionKey(imageURL, dockerConfigJSON string) string {
	authDigest := sha256.Sum256([]byte(dockerConfigJSON))
	return fmt.Sprintf("%s:%x", imageURL, authDigest)
}

func (w *HttpWorker) detectNydusOnce(
	ctx context.Context,
	key string,
	fn func(context.Context) (bool, error),
) (bool, error) {
	resultCh := w.nydusDetectSF.DoChan(key, func() (interface{}, error) {
		sharedCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		lifecycleCtx := w.lifecycleCtx
		if lifecycleCtx == nil {
			lifecycleCtx = context.Background()
		}
		stopLifecycleCancel := context.AfterFunc(lifecycleCtx, cancel)
		defer func() {
			stopLifecycleCancel()
			cancel()
		}()
		return fn(sharedCtx)
	})

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case result := <-resultCh:
		detected, ok := result.Val.(bool)
		if !ok {
			return false, fmt.Errorf("invalid singleflight result type for Nydus detection")
		}
		return detected, result.Err
	}
}
