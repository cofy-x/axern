//go:build linux

package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fixtureKernel struct {
	available  int64
	mountShift uint64
	projects   map[uint64]uint32
	quotas     map[uint32]quota
	rootBroken bool
}

func (f *fixtureKernel) space(*os.File) (int64, int64, error) {
	available := int64(1 << 40)
	if f.available != 0 {
		available = f.available
	}
	return 1 << 40, available, nil
}

func (f *fixtureKernel) root(file *os.File) (directoryIdentity, string, error) {
	if f.rootBroken {
		return directoryIdentity{}, "", errors.New("quota enforcement withdrawn")
	}
	id, err := f.directory(file)
	return id, "fixture-filesystem", err
}
func (f *fixtureKernel) directory(file *os.File) (directoryIdentity, error) {
	id, err := (kernelPlatform{}).directory(file)
	id.Device += f.mountShift
	id.Mount += f.mountShift
	return id, err
}

func (f *fixtureKernel) guestDirectory(file *os.File) error { return file.Chmod(0700) }
func (f *fixtureKernel) quota(_ *os.File, id uint32) (quota, error) {
	q, ok := f.quotas[id]
	if !ok {
		return quota{}, os.ErrNotExist
	}
	return q, nil
}
func (f *fixtureKernel) assign(file *os.File, id uint32) error {
	i, err := f.directory(file)
	if err != nil {
		return err
	}
	f.projects[i.Inode] = id
	return nil
}
func (f *fixtureKernel) limits(_ *os.File, id uint32, blocks, inodes uint64) error {
	f.quotas[id] = quota{Project: id, BlockHard: blocks, InodeHard: inodes}
	return nil
}
func (f *fixtureKernel) verify(file *os.File, id uint32) error {
	i, err := f.directory(file)
	if err != nil {
		return err
	}
	if f.projects[i.Inode] != id {
		return errors.New("project changed")
	}
	return nil
}
func (f *fixtureKernel) remove(root *os.File, name string, want directoryIdentity, project uint32) error {
	dir, err := openDirectory(filepath.Join(root.Name(), name))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	i, err := f.directory(dir)
	dir.Close()
	if err != nil || i.Inode != want.Inode {
		return errors.New("directory replaced")
	}
	if f.projects[i.Inode] != project {
		return errors.New("project replaced")
	}
	return os.RemoveAll(filepath.Join(root.Name(), name))
}
func (f *fixtureKernel) sync(*os.File) error { return nil }

func newFixture(t *testing.T) (*Manager, *fixtureKernel, Spec) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "data")
	require.NoError(t, os.Mkdir(root, 0700))
	m := New(Options{Root: root, MetadataRoot: filepath.Join(base, "owners"), NodeID: "node-one", Capacity: 32 << 20, Inodes: 1024})
	f := &fixtureKernel{projects: map[uint64]uint32{}, quotas: map[uint32]quota{}}
	m.os = f
	capacity, err := m.Capacity()
	require.NoError(t, err)
	spec := Spec{ID: "workspace-one", Namespace: "default", NodeID: "node-one", Capacity: 8 << 20, Inodes: 128, RootIdentity: capacity.Identity}
	return m, f, spec
}

func TestRetainedFilesAndWriterGenerationSurviveReconstruction(t *testing.T) {
	m, kernel, spec := newFixture(t)
	observation, err := m.Provision(spec)
	require.NoError(t, err)
	binding := Binding{Spec: spec, Identity: observation.Identity, Generation: 1}
	path, err := m.Attach(binding, "allocation-one")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(path, "source.txt"), []byte("retained nonce"), 0600))
	_, err = m.Attach(Binding{Spec: spec, Identity: observation.Identity, Generation: 2}, "allocation-two")
	require.ErrorContains(t, err, "another or ambiguous writer")
	restarted := New(m.options)
	restarted.os = kernel
	require.NoError(t, restarted.VerifyAttachment(binding, "allocation-one"))
	receipt, err := restarted.Release(binding, "allocation-one")
	require.NoError(t, err)
	_, err = restarted.Attach(binding, "allocation-one")
	require.ErrorContains(t, err, "already been retired")
	next := binding
	next.Generation = 2
	path, err = restarted.Attach(next, "allocation-two")
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(path, "source.txt"))
	require.NoError(t, err)
	require.Equal(t, "retained nonce", string(data))
	old, ok, err := restarted.Receipt(binding, "allocation-one")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, receipt, old)
	require.Error(t, restarted.Delete(spec, observation.Identity))
	_, err = restarted.Release(next, "allocation-two")
	require.NoError(t, err)
	require.NoError(t, restarted.Delete(spec, observation.Identity))
	require.NoError(t, restarted.Delete(spec, observation.Identity))
	_, err = restarted.Provision(spec)
	require.Error(t, err)
	require.NoError(t, restarted.Acknowledge(receipt))
	_, ok, err = restarted.Receipt(binding, "allocation-one")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestMissingAndReplacedRetainedDataAreNeverRecreated(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "replaced"}[replacement], func(t *testing.T) {
			m, _, spec := newFixture(t)
			observation, err := m.Provision(spec)
			require.NoError(t, err)
			path := filepath.Join(m.options.Root, spec.ID)
			require.NoError(t, os.Rename(path, path+"-original"))
			if replacement {
				require.NoError(t, os.Mkdir(path, 0770))
			}
			_, err = m.Inspect(spec, observation.Identity)
			require.Error(t, err)
			_, err = m.Attach(Binding{Spec: spec, Identity: observation.Identity, Generation: 1}, "allocation-one")
			require.Error(t, err)
			_, err = m.Provision(spec)
			require.Error(t, err)
			if !replacement {
				_, err = os.Lstat(path)
				require.ErrorIs(t, err, os.ErrNotExist)
			}
		})
	}
}

func TestCapacityRemainsChargedWhileDetachedAndUnavailable(t *testing.T) {
	m, kernel, spec := newFixture(t)
	m.options.Capacity = spec.Capacity
	observation, err := m.Provision(spec)
	require.NoError(t, err)
	other := spec
	other.ID = "workspace-two"
	_, err = m.Provision(other)
	require.ErrorContains(t, err, "capacity")
	require.NoError(t, os.Rename(filepath.Join(m.options.Root, spec.ID), filepath.Join(m.options.Root, "unavailable")))
	_, err = m.Provision(other)
	require.ErrorContains(t, err, "capacity")
	require.NoError(t, os.Rename(filepath.Join(m.options.Root, "unavailable"), filepath.Join(m.options.Root, spec.ID)))
	kernel.rootBroken = true
	_, err = m.Inspect(spec, observation.Identity)
	require.ErrorContains(t, err, "enforcement")
	kernel.rootBroken = false
	require.NoError(t, m.Delete(spec, observation.Identity))
	_, err = m.Provision(other)
	require.NoError(t, err)
}

func TestSpecAndPrivateOwnerFences(t *testing.T) {
	m, _, spec := newFixture(t)
	for _, mutate := range []func(*Spec){func(s *Spec) { s.ID = "../escape" }, func(s *Spec) { s.ID = "root" }, func(s *Spec) { s.NodeID = "node-two" }, func(s *Spec) { s.RootIdentity = "replaced" }, func(s *Spec) { s.Capacity++ }, func(s *Spec) { s.Inodes = 0 }} {
		bad := spec
		mutate(&bad)
		_, err := m.Provision(bad)
		require.Error(t, err)
	}
	observation, err := m.Provision(spec)
	require.NoError(t, err)
	_, err = m.Inspect(spec, "other-workspace")
	require.Error(t, err)
	_, err = m.Attach(Binding{Spec: spec, Identity: observation.Identity, Generation: 0}, "allocation-one")
	require.Error(t, err)
	require.NoError(t, os.Chmod(m.options.MetadataRoot, 0755))
	_, err = m.Capacity()
	require.ErrorContains(t, err, "ownership")
}

func TestKernelStorageRejectsUnsupportedFilesystem(t *testing.T) {
	root, err := openDirectory(t.TempDir())
	require.NoError(t, err)
	defer root.Close()
	_, _, err = (kernelPlatform{}).root(root)
	require.Error(t, err)
}

func TestDetachedIdentitySurvivesCurrentKernelMountRenumbering(t *testing.T) {
	m, kernel, spec := newFixture(t)
	observation, err := m.Provision(spec)
	require.NoError(t, err)
	binding := Binding{Spec: spec, Identity: observation.Identity, Generation: 1}
	_, err = m.Attach(binding, "allocation-one")
	require.NoError(t, err)
	_, err = m.Release(binding, "allocation-one")
	require.NoError(t, err)
	kernel.mountShift = 1000
	restarted := New(m.options)
	restarted.os = kernel
	capacity, err := restarted.Capacity()
	require.NoError(t, err)
	require.Equal(t, spec.RootIdentity, capacity.Identity)
	observed, err := restarted.Inspect(spec, observation.Identity)
	require.NoError(t, err)
	require.Equal(t, observation.Identity, observed.Identity)
	binding.Generation = 2
	_, err = restarted.Attach(binding, "allocation-two")
	require.NoError(t, err)
}

func TestNeverActivatedWriterGenerationIsRetiredAfterCleanup(t *testing.T) {
	m, _, spec := newFixture(t)
	observation, err := m.Provision(spec)
	require.NoError(t, err)
	binding := Binding{Spec: spec, Identity: observation.Identity, Generation: 1}
	_, err = m.Release(binding, "allocation-never-started")
	require.NoError(t, err)
	require.Error(t, m.CheckAttachment(binding, "allocation-never-started"))
	binding.Generation = 2
	require.NoError(t, m.CheckAttachment(binding, "allocation-new"))
	_, err = m.Attach(binding, "allocation-new")
	require.NoError(t, err)
}

func TestDelayedAcknowledgementDoesNotChangeNewWriter(t *testing.T) {
	m, _, spec := newFixture(t)
	observation, err := m.Provision(spec)
	require.NoError(t, err)
	a := Binding{Spec: spec, Identity: observation.Identity, Generation: 1}
	_, err = m.Attach(a, "allocation-one")
	require.NoError(t, err)
	receipt, err := m.Release(a, "allocation-one")
	require.NoError(t, err)
	b := a
	b.Generation = 2
	_, err = m.Attach(b, "allocation-two")
	require.NoError(t, err)
	require.NoError(t, m.Acknowledge(receipt))
	require.NoError(t, m.VerifyAttachment(b, "allocation-two"))
	require.Error(t, m.Delete(spec, observation.Identity))
}

func TestQuotaWithdrawalAndChangedIdentityFailClosed(t *testing.T) {
	m, kernel, spec := newFixture(t)
	observation, err := m.Provision(spec)
	require.NoError(t, err)
	owner, err := m.readOwner(spec.ID)
	require.NoError(t, err)
	q := kernel.quotas[owner.Project]
	q.BlockHard = 0
	kernel.quotas[owner.Project] = q
	_, err = m.Inspect(spec, observation.Identity)
	require.Error(t, err)
	require.Error(t, m.CheckAttachment(Binding{Spec: spec, Identity: observation.Identity, Generation: 1}, "allocation-one"))
	q.BlockHard = uint64(spec.Capacity / 512)
	kernel.quotas[owner.Project] = q
	require.NoError(t, m.Delete(spec, observation.Identity))
	next := spec
	next.ID = "workspace-two"
	_, err = m.Provision(next)
	require.NoError(t, err)
	other, err := m.readOwner(next.ID)
	require.NoError(t, err)
	require.Greater(t, other.Project, owner.Project)
}

func TestSharedFilesystemCapacityIsExcludedFromEphemeralAdmission(t *testing.T) {
	m, _, _ := newFixture(t)
	m.options.FilestoreRoot = t.TempDir()
	m.options.FilestoreReserve = m.options.Capacity
	_, err := m.Capacity()
	require.ErrorContains(t, err, "operator reserve")
	m.options.FilestoreReserve += 1 << 30
	_, err = m.Capacity()
	require.NoError(t, err)
}

func TestCancelBeforeAdmissionDurablyWithdrawsWriter(t *testing.T) {
	m, kernel, spec := newFixture(t)
	observation, err := m.Provision(spec)
	require.NoError(t, err)
	binding := Binding{Spec: spec, Identity: observation.Identity, Generation: 1}
	require.NoError(t, m.BeginCleanup(binding, "allocation-delayed"))
	restarted := New(m.options)
	restarted.os = kernel
	require.Error(t, restarted.CheckAttachment(binding, "allocation-delayed"))
	_, err = restarted.Attach(binding, "allocation-delayed")
	require.Error(t, err)
	require.NoError(t, restarted.VerifyCleanupOwnership(binding, "allocation-delayed"))
	_, err = restarted.Release(binding, "allocation-delayed")
	require.NoError(t, err)
	binding.Generation = 2
	_, err = restarted.Attach(binding, "allocation-new")
	require.NoError(t, err)
}

type blockedRemoval struct {
	*fixtureKernel
	entered, release chan struct{}
}

func (f *blockedRemoval) remove(root *os.File, name string, want directoryIdentity, project uint32) error {
	close(f.entered)
	<-f.release
	return f.fixtureKernel.remove(root, name, want, project)
}

func TestUnrelatedDeletionDoesNotBlockActiveWriterVerification(t *testing.T) {
	m, kernel, spec := newFixture(t)
	a, err := m.Provision(spec)
	require.NoError(t, err)
	other := spec
	other.ID = "workspace-two"
	b, err := m.Provision(other)
	require.NoError(t, err)
	binding := Binding{Spec: other, Identity: b.Identity, Generation: 1}
	_, err = m.Attach(binding, "allocation-two")
	require.NoError(t, err)
	blocked := &blockedRemoval{fixtureKernel: kernel, entered: make(chan struct{}), release: make(chan struct{})}
	m.os = blocked
	deleted := make(chan error, 1)
	go func() { deleted <- m.Delete(spec, a.Identity) }()
	<-blocked.entered
	verified := make(chan error, 1)
	go func() { verified <- m.VerifyAttachment(binding, "allocation-two") }()
	select {
	case err := <-verified:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		close(blocked.release)
		<-deleted
		t.Fatal("unrelated recursive deletion blocked active writer verification")
	}
	close(blocked.release)
	require.NoError(t, <-deleted)
}

func TestNewProvisioningRejectsFilesystemExhaustionWithoutLosingRetainedData(t *testing.T) {
	m, kernel, spec := newFixture(t)
	a, err := m.Provision(spec)
	require.NoError(t, err)
	kernel.available = 1 << 30
	other := spec
	other.ID = "workspace-two"
	_, err = m.Provision(other)
	require.ErrorContains(t, err, "free space")
	_, err = m.Inspect(spec, a.Identity)
	require.NoError(t, err, "existing Workspace remains inspectable without requiring its entire granted capacity free")
}

func TestOversizedOwnerUpdatePreservesReadablePriorState(t *testing.T) {
	m, _, spec := newFixture(t)
	observation, err := m.Provision(spec)
	require.NoError(t, err)
	current, err := m.readOwner(spec.ID)
	require.NoError(t, err)
	current.Receipts = map[string]Receipt{"oversized": {WorkspaceID: spec.ID, Allocation: strings.Repeat("a", maxOwnerRecordBytes), Generation: 1, Identity: observation.Identity}}
	require.ErrorContains(t, m.writeOwner(current), "record capacity")
	unchanged, err := m.readOwner(spec.ID)
	require.NoError(t, err)
	require.Empty(t, unchanged.Receipts)
	_, err = m.Inspect(spec, observation.Identity)
	require.NoError(t, err)
}

func TestNamespaceIsMetadataAndNotAHostPathComponent(t *testing.T) {
	for _, namespace := range []string{"team.alpha", "中文团队", "org/team"} {
		t.Run(namespace, func(t *testing.T) {
			m, _, spec := newFixture(t)
			spec.Namespace = namespace
			_, err := m.Provision(spec)
			require.NoError(t, err)
		})
	}
}

func TestEmptyRootMetadataLossCreatesNewAuthorityEpoch(t *testing.T) {
	m, kernel, spec := newFixture(t)
	observation, err := m.Provision(spec)
	require.NoError(t, err)
	require.NoError(t, m.Delete(spec, observation.Identity))
	require.NoError(t, os.RemoveAll(m.options.MetadataRoot))
	restarted := New(m.options)
	restarted.os = kernel
	capacity, err := restarted.Capacity()
	require.NoError(t, err)
	require.NotEqual(t, spec.RootIdentity, capacity.Identity)
	_, err = restarted.Provision(spec)
	require.Error(t, err, "late provisioning cannot acquire an empty replacement ownership domain")
	fresh := spec
	fresh.ID = "workspace-new-domain"
	fresh.RootIdentity = capacity.Identity
	_, err = restarted.Provision(fresh)
	require.NoError(t, err)
}

func TestMissingOwnerEpochIsRejectedWithoutRepair(t *testing.T) {
	m, _, _ := newFixture(t)
	var record rootRecord
	require.NoError(t, m.read("root.json", &record))
	record.Epoch = ""
	require.NoError(t, m.write("root.json", &record))
	_, err := m.Capacity()
	require.ErrorContains(t, err, "ownership")
	var unchanged rootRecord
	require.NoError(t, m.read("root.json", &unchanged))
	require.Empty(t, unchanged.Epoch)
}
