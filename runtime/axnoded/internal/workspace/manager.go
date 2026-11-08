package workspace

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Spec is the immutable retained-storage contract granted by controld.
type Spec struct {
	ID           string `json:"id"`
	Namespace    string `json:"namespace"`
	NodeID       string `json:"node_id"`
	Capacity     int64  `json:"capacity_bytes"`
	Inodes       int64  `json:"inode_limit"`
	RootIdentity string `json:"root_identity"`
}

type Binding struct {
	Spec       Spec
	Identity   string
	Generation uint64
}

type Receipt struct {
	WorkspaceID string `json:"workspace_id"`
	Allocation  string `json:"allocation_id"`
	Generation  uint64 `json:"generation"`
	Identity    string `json:"storage_identity"`
}

type Observation struct {
	Identity string
	Bytes    int64
	Inodes   int64
}

type Options struct {
	FilestoreRoot              string
	FilestoreReserve           int64
	Root, MetadataRoot, NodeID string
	Capacity, Inodes           int64
}

type directoryIdentity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Mount  uint64 `json:"mount"`
}

type rootRecord struct {
	Epoch       string            `json:"epoch"`
	Reserve     int64             `json:"-"`
	Version     int               `json:"version"`
	Identity    string            `json:"identity"`
	Directory   directoryIdentity `json:"directory"`
	NextProject uint32            `json:"next_project"`
}

type owner struct {
	Version    int                `json:"version"`
	Spec       Spec               `json:"spec"`
	Project    uint32             `json:"project"`
	Directory  directoryIdentity  `json:"directory"`
	Identity   string             `json:"identity"`
	State      string             `json:"state"`
	Generation uint64             `json:"generation"`
	Allocation string             `json:"allocation_id,omitempty"`
	Retiring   bool               `json:"retiring,omitempty"`
	Receipts   map[string]Receipt `json:"receipts,omitempty"`
}

type quota struct {
	Project, Flags               uint32
	BlockHard, BlockSoft, Blocks uint64
	InodeHard, InodeSoft, Inodes uint64
}

// platform is the narrow kernel boundary used by managed storage, not a
// selectable backend. Production always uses the Linux XFS implementation.
type platform interface {
	root(*os.File) (directoryIdentity, string, error)
	space(*os.File) (int64, int64, error)
	directory(*os.File) (directoryIdentity, error)
	quota(*os.File, uint32) (quota, error)
	assign(*os.File, uint32) error
	guestDirectory(*os.File) error
	limits(*os.File, uint32, uint64, uint64) error
	verify(*os.File, uint32) error
	remove(*os.File, string, directoryIdentity, uint32) error
	sync(*os.File) error
}

type Manager struct {
	rootMu  sync.Mutex
	locksMu sync.Mutex
	locks   map[string]*workspaceLock
	mu      sync.Mutex
	options Options
	os      platform
}

type workspaceLock struct {
	mu   sync.Mutex
	refs int
}

var identityName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

const maxOwnerRecordBytes = 1 << 20
const maxNamespaceBytes = 4096

func New(options Options) *Manager { return &Manager{options: options, os: kernelPlatform{}} }

func (m *Manager) Enabled() bool { return m != nil && m.options.Root != "" }

func (m *Manager) Capacity() (Observation, error) {
	if !m.Enabled() {
		return Observation{}, errors.New("persistent Workspace storage is disabled")
	}
	root, record, err := m.openRoot()
	if err != nil {
		return Observation{}, err
	}
	defer root.Close()
	return Observation{Identity: record.Identity, Bytes: m.options.Capacity, Inodes: m.options.Inodes}, nil
}

func (m *Manager) Provision(spec Spec) (Observation, error) {
	unlock := m.lockWorkspace(spec.ID)
	defer unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	root, record, err := m.openRoot()
	if err != nil {
		return Observation{}, err
	}
	defer root.Close()
	if err := m.validateSpec(spec, record); err != nil {
		return Observation{}, err
	}
	current, err := m.readOwner(spec.ID)
	if err == nil {
		if current.Spec != spec || current.State == "deleted" || current.State == "deleting" {
			return Observation{}, errors.New("Workspace provisioning conflicts with retained ownership")
		}
		if current.State == "ready" {
			return m.observe(root, current, current.Identity)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Observation{}, err
	} else {
		if err := m.admitCapacity(root, spec, record.Reserve); err != nil {
			return Observation{}, err
		}
		if record.NextProject == math.MaxUint32 {
			return Observation{}, errors.New("Workspace project identity space is exhausted")
		}
		project := record.NextProject
		if project == 0 {
			return Observation{}, errors.New("invalid Workspace project allocator")
		}
		q, err := m.os.quota(root, project)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Observation{}, fmt.Errorf("inspect unassigned Workspace project: %w", err)
		}
		if q.Blocks != 0 || q.Inodes != 0 || q.BlockHard != 0 || q.InodeHard != 0 || q.BlockSoft != 0 || q.InodeSoft != 0 {
			return Observation{}, errors.New("Workspace project identity is already charged")
		}
		record.NextProject++
		if err := m.write("root.json", record); err != nil {
			return Observation{}, err
		}
		current = &owner{Version: 1, Spec: spec, Project: project, State: "provisioning"}
		if err := m.writeOwner(current); err != nil {
			return Observation{}, err
		}
	}
	path := filepath.Join(m.options.Root, spec.ID)
	if current.Directory.Inode == 0 {
		// A present tree without a durable inode is an ambiguous interrupted
		// provisioning operation. Never adopt it or replace it with empty data.
		if err := os.Mkdir(path, 0700); err != nil {
			return Observation{}, fmt.Errorf("create new Workspace tree: %w", err)
		}
		dir, err := openDirectory(path)
		if err != nil {
			return Observation{}, err
		}
		current.Directory, err = m.os.directory(dir)
		closeErr := dir.Close()
		if err != nil || closeErr != nil {
			return Observation{}, errors.Join(err, closeErr)
		}
		if err := m.os.sync(root); err != nil {
			return Observation{}, err
		}
		if err := m.writeOwner(current); err != nil {
			return Observation{}, err
		}
	}
	dir, err := m.openData(root, current)
	if err != nil {
		return Observation{}, err
	}
	defer dir.Close()
	if err := m.os.assign(dir, current.Project); err != nil {
		return Observation{}, err
	}
	if err := m.os.limits(root, current.Project, uint64(spec.Capacity/512), uint64(spec.Inodes)); err != nil {
		return Observation{}, err
	}
	if err := m.os.guestDirectory(dir); err != nil {
		return Observation{}, err
	}
	if err := m.os.sync(dir); err != nil {
		return Observation{}, err
	}
	current.Identity = fingerprint(struct {
		Spec    Spec
		Inode   uint64
		Project uint32
	}{current.Spec, current.Directory.Inode, current.Project})
	current.State = "ready"
	if err := m.writeOwner(current); err != nil {
		return Observation{}, err
	}
	return m.observe(root, current, current.Identity)
}

func (m *Manager) Inspect(spec Spec, identity string) (Observation, error) {
	unlock := m.lockWorkspace(spec.ID)
	defer unlock()
	root, record, err := m.openRoot()
	if err != nil {
		return Observation{}, err
	}
	defer root.Close()
	if err := m.validateSpec(spec, record); err != nil {
		return Observation{}, err
	}
	current, err := m.readOwner(spec.ID)
	if err != nil {
		return Observation{}, err
	}
	if current.Spec != spec || current.State != "ready" {
		return Observation{}, errors.New("Workspace is not available under the granted storage contract")
	}
	return m.observe(root, current, identity)
}

// Attach records a monotonic writer fence before the runtime can access data.
// Only an exact idempotent replay may use an already-attached generation.
func (m *Manager) Attach(binding Binding, allocationID string) (string, error) {
	unlock := m.lockWorkspace(binding.Spec.ID)
	defer unlock()
	if !identityName.MatchString(allocationID) || binding.Generation == 0 {
		return "", errors.New("Workspace attachment requires exact Allocation and generation")
	}
	root, record, err := m.openRoot()
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err := m.validateSpec(binding.Spec, record); err != nil {
		return "", err
	}
	current, err := m.readOwner(binding.Spec.ID)
	if err != nil {
		return "", err
	}
	if current.Spec != binding.Spec || current.State != "ready" {
		return "", errors.New("Workspace attachment does not match available ownership")
	}
	if current.Retiring {
		return "", errors.New("Workspace writer is retiring")
	}
	if _, err := m.observe(root, current, binding.Identity); err != nil {
		return "", err
	}
	if current.Allocation != "" {
		if current.Allocation != allocationID || current.Generation != binding.Generation {
			return "", errors.New("Workspace has another or ambiguous writer")
		}
	} else {
		if binding.Generation <= current.Generation {
			return "", errors.New("Workspace writer generation has already been retired")
		}
		current.Generation, current.Allocation = binding.Generation, allocationID
		if err := m.writeOwner(current); err != nil {
			return "", err
		}
	}
	return filepath.Join(m.options.Root, binding.Spec.ID), nil
}

func (m *Manager) VerifyAttachment(binding Binding, allocationID string) error {
	unlock := m.lockWorkspace(binding.Spec.ID)
	defer unlock()
	root, record, err := m.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := m.validateSpec(binding.Spec, record); err != nil {
		return err
	}
	current, err := m.readOwner(binding.Spec.ID)
	if err != nil {
		return err
	}
	if current.Spec != binding.Spec || current.State != "ready" || current.Retiring || current.Allocation != allocationID || current.Generation != binding.Generation {
		return errors.New("Workspace writer ownership does not match the admitted Allocation")
	}
	_, err = m.observe(root, current, binding.Identity)
	return err
}

// CheckAttachment rejects retired commands before the caller persists a new
// Allocation intent. Attach repeats this check while committing the fence.
func (m *Manager) CheckAttachment(binding Binding, allocationID string) error {
	unlock := m.lockWorkspace(binding.Spec.ID)
	defer unlock()
	if !identityName.MatchString(allocationID) || binding.Generation == 0 {
		return errors.New("invalid Workspace writer identity")
	}
	root, record, err := m.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := m.validateSpec(binding.Spec, record); err != nil {
		return err
	}
	current, err := m.readOwner(binding.Spec.ID)
	if err != nil {
		return err
	}
	if current.Spec != binding.Spec || current.State != "ready" {
		return errors.New("Workspace ownership is unavailable")
	}
	if current.Retiring {
		return errors.New("Workspace writer is retiring")
	}
	if current.Allocation != "" {
		if current.Allocation != allocationID || current.Generation != binding.Generation {
			return errors.New("Workspace already has a writer")
		}
	} else if binding.Generation <= current.Generation {
		return errors.New("Workspace writer generation has been retired")
	}
	_, err = m.observe(root, current, binding.Identity)
	return err
}

// Release must only be called after exact runtime/Gofer/mount and Allocation
// resource cleanup. The receipt and retired writer fence commit atomically.
func (m *Manager) Release(binding Binding, allocationID string) (Receipt, error) {
	unlock := m.lockWorkspace(binding.Spec.ID)
	defer unlock()
	want := Receipt{binding.Spec.ID, allocationID, binding.Generation, binding.Identity}
	current, err := m.readOwner(binding.Spec.ID)
	if err != nil {
		return Receipt{}, err
	}
	if existing, ok := current.Receipts[allocationID]; ok {
		if existing != want {
			return Receipt{}, errors.New("Workspace cleanup receipt conflicts with the request")
		}
		return existing, nil
	}
	if current.Spec != binding.Spec || current.Identity != binding.Identity || (current.Allocation != "" && (current.Allocation != allocationID || current.Generation != binding.Generation)) || (current.Allocation == "" && current.Generation >= binding.Generation) {
		return Receipt{}, errors.New("Workspace cleanup does not own the exact writer")
	}
	if current.Receipts == nil {
		current.Receipts = make(map[string]Receipt)
	}
	current.Receipts[allocationID] = want
	current.Generation = binding.Generation
	current.Allocation = ""
	current.Retiring = false
	if err := m.writeOwner(current); err != nil {
		return Receipt{}, err
	}
	return want, nil
}

// BeginCleanup withdraws future create authority, including a cancel that
// precedes node admission. It never establishes runtime or release evidence.
func (m *Manager) BeginCleanup(binding Binding, allocationID string) error {
	unlock := m.lockWorkspace(binding.Spec.ID)
	defer unlock()
	if !identityName.MatchString(allocationID) || binding.Generation == 0 {
		return errors.New("invalid Workspace cleanup identity")
	}
	current, err := m.readOwner(binding.Spec.ID)
	if err != nil {
		return err
	}
	if current.Spec != binding.Spec || current.Identity != binding.Identity || current.State != "ready" {
		return errors.New("Workspace cleanup ownership mismatch")
	}
	if receipt, ok := current.Receipts[allocationID]; ok {
		if receipt != (Receipt{binding.Spec.ID, allocationID, binding.Generation, binding.Identity}) {
			return errors.New("Workspace cleanup receipt mismatch")
		}
		return nil
	}
	if current.Allocation != "" {
		if current.Allocation != allocationID || current.Generation != binding.Generation {
			return errors.New("Workspace cleanup does not own current writer")
		}
	} else if binding.Generation <= current.Generation {
		return errors.New("Workspace cleanup generation is retired")
	}
	current.Allocation = allocationID
	current.Generation = binding.Generation
	current.Retiring = true
	return m.writeOwner(current)
}

// A stopped runtime may retain its exact cleanup debt even when bytes or quota
// are unavailable. This check grants no authority to execute or attach.
func (m *Manager) VerifyCleanupOwnership(binding Binding, allocationID string) error {
	unlock := m.lockWorkspace(binding.Spec.ID)
	defer unlock()
	current, err := m.readOwner(binding.Spec.ID)
	if err != nil {
		return err
	}
	if current.Spec != binding.Spec || current.Identity != binding.Identity || current.Allocation != allocationID || current.Generation != binding.Generation {
		return errors.New("Workspace terminal cleanup ownership mismatch")
	}
	return nil
}

func (m *Manager) Receipt(binding Binding, allocationID string) (Receipt, bool, error) {
	unlock := m.lockWorkspace(binding.Spec.ID)
	defer unlock()
	current, err := m.readOwner(binding.Spec.ID)
	if err != nil {
		return Receipt{}, false, err
	}
	value, ok := current.Receipts[allocationID]
	if ok && (current.Spec != binding.Spec || value != (Receipt{binding.Spec.ID, allocationID, binding.Generation, binding.Identity})) {
		return Receipt{}, false, errors.New("Workspace cleanup receipt does not match the request")
	}
	return value, ok, nil
}

func (m *Manager) Acknowledge(receipt Receipt) error {
	unlock := m.lockWorkspace(receipt.WorkspaceID)
	defer unlock()
	current, err := m.readOwner(receipt.WorkspaceID)
	if err != nil {
		return err
	}
	if existing, ok := current.Receipts[receipt.Allocation]; ok {
		if existing != receipt {
			return errors.New("Workspace cleanup acknowledgement does not match receipt")
		}
		delete(current.Receipts, receipt.Allocation)
		return m.writeOwner(current)
	}
	return nil
}

func (m *Manager) Delete(spec Spec, identity string) error {
	unlock := m.lockWorkspace(spec.ID)
	defer unlock()
	root, record, err := m.openRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	if err := m.validateSpec(spec, record); err != nil {
		return err
	}
	current, err := m.readOwner(spec.ID)
	if err != nil {
		return err
	}
	if current.Spec != spec || current.Identity != identity || identity == "" || current.Allocation != "" {
		return errors.New("Workspace deletion lacks exact detached ownership")
	}
	if current.State == "deleted" {
		return nil
	}
	if current.State != "deleting" {
		if _, err := m.observe(root, current, identity); err != nil {
			return err
		}
		current.State = "deleting"
		if err := m.writeOwner(current); err != nil {
			return err
		}
	}
	if err := m.os.remove(root, spec.ID, current.Directory, current.Project); err != nil {
		return err
	}
	if err := m.os.sync(root); err != nil {
		return err
	}
	q, err := m.os.quota(root, current.Project)
	if (err != nil && !errors.Is(err, os.ErrNotExist)) || q.Blocks != 0 || q.Inodes != 0 {
		return fmt.Errorf("Workspace project still has charged data after deletion: %w", errors.Join(err, errors.New("charge not confirmed empty")))
	}
	if err := m.os.limits(root, current.Project, 0, 0); err != nil {
		return err
	}
	current.State = "deleted"
	return m.writeOwner(current)
}

func (m *Manager) validateSpec(spec Spec, record *rootRecord) error {
	if !identityName.MatchString(spec.ID) || spec.ID == "root" || spec.Namespace == "" || len(spec.Namespace) > maxNamespaceBytes || strings.TrimSpace(spec.Namespace) != spec.Namespace || spec.NodeID != m.options.NodeID || spec.NodeID == "" || spec.Capacity <= 0 || spec.Capacity%512 != 0 || spec.Inodes <= 0 || spec.RootIdentity == "" || spec.RootIdentity != record.Identity {
		return errors.New("Workspace specification does not match this node's qualified storage")
	}
	return nil
}

func (m *Manager) observe(root *os.File, current *owner, identity string) (Observation, error) {
	if identity == "" || identity != current.Identity {
		return Observation{}, errors.New("Workspace storage identity changed")
	}
	dir, err := m.openData(root, current)
	if err != nil {
		return Observation{}, err
	}
	defer dir.Close()
	if err := m.os.verify(dir, current.Project); err != nil {
		return Observation{}, err
	}
	q, err := m.os.quota(root, current.Project)
	if err != nil {
		return Observation{}, err
	}
	if q.Project != current.Project || q.BlockHard != uint64(current.Spec.Capacity/512) || q.BlockSoft != 0 || q.InodeHard != uint64(current.Spec.Inodes) || q.InodeSoft != 0 || q.Blocks > q.BlockHard || q.Inodes > q.InodeHard || q.Blocks > math.MaxInt64/512 || q.Inodes > math.MaxInt64 {
		return Observation{}, errors.New("Workspace quota identity or finite limits changed")
	}
	return Observation{Identity: identity, Bytes: int64(q.Blocks) * 512, Inodes: int64(q.Inodes)}, nil
}

func (m *Manager) openData(root *os.File, current *owner) (*os.File, error) {
	dir, err := openChildDirectory(root, current.Spec.ID)
	if err != nil {
		return nil, fmt.Errorf("retained Workspace data is unavailable: %w", err)
	}
	identity, err := m.os.directory(dir)
	rootIdentity, rootErr := m.os.directory(root)
	if err != nil || rootErr != nil || identity.Inode != current.Directory.Inode || identity.Device != rootIdentity.Device || identity.Mount != rootIdentity.Mount {
		dir.Close()
		return nil, errors.New("retained Workspace directory identity changed")
	}
	return dir, nil
}

func (m *Manager) openRoot() (*os.File, *rootRecord, error) {
	m.rootMu.Lock()
	defer m.rootMu.Unlock()
	if !m.Enabled() || m.options.Capacity <= 0 || m.options.Inodes <= 0 || m.options.NodeID == "" {
		return nil, nil, errors.New("persistent Workspace storage is not configured")
	}
	if m.options.Root == m.options.MetadataRoot || strings.HasPrefix(m.options.Root+"/", m.options.MetadataRoot+"/") || strings.HasPrefix(m.options.MetadataRoot+"/", m.options.Root+"/") {
		return nil, nil, errors.New("Workspace data and ownership roots must not overlap")
	}
	root, err := openDirectory(m.options.Root)
	if err != nil {
		return nil, nil, fmt.Errorf("Workspace storage root is unavailable: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			root.Close()
		}
	}()
	identity, filesystem, err := m.os.root(root)
	if err != nil {
		return nil, nil, err
	}
	reserve, err := operatorReserveForFilesystem(root, m.options.FilestoreRoot, m.options.FilestoreReserve, m.options.Capacity)
	if err != nil {
		return nil, nil, err
	}
	total, _, err := m.os.space(root)
	if err != nil {
		return nil, nil, err
	}
	if total <= reserve || m.options.Capacity > total-reserve {
		return nil, nil, errors.New("Workspace capacity budget exceeds its filesystem after operator reserve")
	}
	info, err := root.Stat()
	if err != nil || info.Mode().Perm()&0077 != 0 {
		return nil, nil, errors.New("Workspace storage root must remain private")
	}
	if err := ensureMetadataDirectory(m.options.MetadataRoot); err != nil {
		return nil, nil, err
	}
	metadata, err := openDirectory(m.options.MetadataRoot)
	if err != nil {
		return nil, nil, err
	}
	metadataInfo, err := metadata.Stat()
	metadata.Close()
	if err != nil || metadataInfo.Mode().Perm()&0077 != 0 {
		return nil, nil, errors.New("Workspace ownership records must remain private")
	}
	var record rootRecord
	if err := m.read("root.json", &record); err == nil {
		decodedEpoch, epochErr := hex.DecodeString(record.Epoch)
		if record.Version != 1 || epochErr != nil || len(decodedEpoch) != 32 || record.Directory.Inode != identity.Inode || record.Identity != rootFingerprint(identity.Inode, filesystem, record.Epoch) || record.NextProject == 0 {
			return nil, nil, errors.New("Workspace root ownership or filesystem identity changed")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		entries, err := root.ReadDir(1)
		if err != io.EOF || len(entries) != 0 {
			return nil, nil, errors.New("cannot adopt a nonempty Workspace root without ownership records")
		}
		var randomEpoch [32]byte
		if _, err := rand.Read(randomEpoch[:]); err != nil {
			return nil, nil, err
		}
		epoch := hex.EncodeToString(randomEpoch[:])
		record = rootRecord{Version: 1, Epoch: epoch, Directory: identity, Identity: rootFingerprint(identity.Inode, filesystem, epoch), NextProject: 1_000_000}
		if err := m.write("root.json", &record); err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, err
	}
	record.Reserve = reserve
	ok = true
	return root, &record, nil
}

func (m *Manager) admitCapacity(root *os.File, spec Spec, reserve int64) error {
	_, available, err := m.os.space(root)
	if err != nil {
		return err
	}
	if available <= reserve || spec.Capacity > available-reserve {
		return errors.New("Workspace filesystem has insufficient free space after operator reserve")
	}
	entries, err := os.ReadDir(m.options.MetadataRoot)
	if err != nil {
		return err
	}
	bytes, inodes := spec.Capacity, spec.Inodes
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" || entry.Name() == "root.json" {
			continue
		}
		var current owner
		if err := m.read(entry.Name(), &current); err != nil {
			return err
		}
		if current.Version != 1 || current.Spec.ID+".json" != entry.Name() {
			return errors.New("invalid Workspace ownership record")
		}
		if current.State != "deleted" {
			if current.Spec.Capacity <= 0 || current.Spec.Inodes <= 0 || current.Spec.Capacity > m.options.Capacity-bytes || current.Spec.Inodes > m.options.Inodes-inodes {
				return errors.New("Workspace retained capacity is exhausted")
			}
			bytes += current.Spec.Capacity
			inodes += current.Spec.Inodes
		}
	}
	if bytes > m.options.Capacity || inodes > m.options.Inodes {
		return errors.New("Workspace retained capacity is exhausted")
	}
	return nil
}

func (m *Manager) readOwner(id string) (*owner, error) {
	if !identityName.MatchString(id) || id == "root" {
		return nil, errors.New("invalid Workspace identity")
	}
	var value owner
	if err := m.read(id+".json", &value); err != nil {
		return nil, err
	}
	if value.Version != 1 || value.Spec.ID != id || value.Project == 0 {
		return nil, errors.New("invalid Workspace ownership record")
	}
	return &value, nil
}

func (m *Manager) writeOwner(value *owner) error { return m.write(value.Spec.ID+".json", value) }

func (m *Manager) read(name string, value any) error {
	f, err := openRegular(filepath.Join(m.options.MetadataRoot, name))
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, maxOwnerRecordBytes))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return fmt.Errorf("decode Workspace ownership: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("Workspace ownership has trailing data")
	}
	return nil
}

func (m *Manager) write(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data)+1 > maxOwnerRecordBytes {
		return errors.New("Workspace ownership record capacity is exhausted; pending acknowledgements must be reconciled")
	}
	f, err := os.CreateTemp(m.options.MetadataRoot, ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), filepath.Join(m.options.MetadataRoot, name)); err != nil {
		return err
	}
	dir, err := openDirectory(m.options.MetadataRoot)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func fingerprint(value any) string {
	data, _ := json.Marshal(value)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func rootFingerprint(inode uint64, filesystem, epoch string) string {
	return fingerprint(struct {
		Inode      uint64
		Filesystem string
		Epoch      string
	}{inode, filesystem, epoch})
}

func (m *Manager) lockWorkspace(id string) func() {
	m.locksMu.Lock()
	if m.locks == nil {
		m.locks = make(map[string]*workspaceLock)
	}
	lock := m.locks[id]
	if lock == nil {
		lock = &workspaceLock{}
		m.locks[id] = lock
	}
	// Waiting callers retain the same entry before joining its mutex. Removing
	// a key while a waiter exists could create two simultaneous owner lanes.
	lock.refs++
	m.locksMu.Unlock()
	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		m.locksMu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(m.locks, id)
		}
		m.locksMu.Unlock()
	}
}
