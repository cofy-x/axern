package workloadtls

import (
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
)

// PublishBundle replaces key and certificate as one durable unit. Callers must
// serialize enrollment/renewal for this identity. The parent is operator-owned.
func PublishBundle(path string, certificate, privateKey []byte) error {
	if _, err := tls.X509KeyPair(certificate, privateKey); err != nil {
		return fmt.Errorf("validate workload bundle: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".workload-bundle-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(append(append([]byte{}, certificate...), privateKey...)); err != nil {
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
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}
