package hostlinux

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

const cgroupMemoryProbeLimitBytes int64 = 64 << 20

func probeCgroupMemoryLimit(driver os2.CgroupDriver, rootName string, verify func(string, int64) error) (result error) {
	if driver == nil {
		return fmt.Errorf("cgroup memory probe driver is nil")
	}
	if verify == nil {
		return fmt.Errorf("cgroup memory probe verifier is nil")
	}
	rootName = strings.Trim(strings.TrimSpace(rootName), "/")
	if rootName == "" {
		return fmt.Errorf("cgroup memory probe root is empty")
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("generate cgroup memory probe ID: %w", err)
	}
	group := filepath.Join("/", rootName, ".memory-limit-probe-"+hex.EncodeToString(nonce[:]))
	defer func() {
		if err := driver.Remove(group); err != nil {
			result = errors.Join(result, fmt.Errorf("remove cgroup memory probe %s: %w", group, err))
		}
	}()
	limit := cgroupMemoryProbeLimitBytes
	cgroup, err := driver.Create(group, &spec.LinuxResources{})
	if err != nil {
		return fmt.Errorf("create cgroup memory probe %s: %w", group, err)
	}
	if cgroup == nil {
		return fmt.Errorf("create cgroup memory probe %s returned no handle", group)
	}
	if err := cgroup.Update(&spec.LinuxResources{Memory: &spec.LinuxMemory{Limit: &limit}}); err != nil {
		return fmt.Errorf("write cgroup memory probe limit: %w", err)
	}
	if err := verify(RuntimeCgroupPath(driver, group), limit); err != nil {
		return fmt.Errorf("read back cgroup memory probe limit: %w", err)
	}
	return nil
}
