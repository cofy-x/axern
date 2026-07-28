//go:build !linux

package rootfsview

func rootfsPathReadOnly(rootDir string) (bool, error) {
	return false, nil
}
