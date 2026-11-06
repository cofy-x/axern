//go:build !linux

package hostlinux

import "fmt"

func ConfigureCgroupPIDDomain(string, string, int64) error {
	return fmt.Errorf("host task cgroup enforcement requires Linux")
}

func VerifyCgroupPIDDomain(string, string, int64) error {
	return fmt.Errorf("host task cgroup enforcement requires Linux")
}
