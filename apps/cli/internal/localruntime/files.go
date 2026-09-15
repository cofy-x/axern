package localruntime

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	gossh "golang.org/x/crypto/ssh"
)

func DataDir() (string, error) {
	if root := strings.TrimSpace(os.Getenv("AXERN_HOME")); root != "" {
		return filepath.Join(root, "local"), nil
	}
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "Axern", "local"), nil
	}
	root := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(root, "axern", "local"), nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".axern-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func randomHex(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func randomBase64(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(value), nil
}

func validSecretsMasterKey(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) == 32 {
		return true
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func ensurePKI(dir string) error {
	return (workloadtls.Bootstrap{Directory: dir, Cluster: "axern.local", DNSNames: []string{"localhost", "host.docker.internal", "controld", "gatewayd", "tunneld", "registry"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}).Ensure(false)
}

func validCertificateSet(dir string) bool {
	issuer, err := (workloadtls.FileIssuer{BundlePath: filepath.Join(dir, "private", "signer.pem"), Cluster: "axern.local"}).Load()
	if err != nil {
		return false
	}
	roots := x509.NewCertPool()
	trust, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil || !roots.AppendCertsFromPEM(trust) {
		return false
	}
	if _, err := issuer.Certificate.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		return false
	}
	for _, name := range []string{"controld", "gatewayd", "tunneld"} {
		data, err := os.ReadFile(filepath.Join(dir, name+".pem"))
		if err != nil {
			return false
		}
		pair, err := tls.X509KeyPair(data, data)
		if err != nil {
			return false
		}
		cert, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return false
		}
		identity, err := workloadtls.FromCertificate(cert, "axern.local")
		if err != nil || identity.Role != name {
			return false
		}
		if _, err := cert.Verify(x509.VerifyOptions{Roots: roots}); err != nil {
			return false
		}
	}
	_, err = tls.LoadX509KeyPair(filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key"))
	return err == nil
}

func ensureSSH(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, name := range []string{"gateway_host_ed25519", "gateway_client_ed25519"} {
		path := filepath.Join(dir, name)
		if validSSHPrivateKey(path) {
			continue
		}
		pub, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		encoded, err := x509.MarshalPKCS8PrivateKey(private)
		if err != nil {
			return err
		}
		if err := writeAtomic(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0o600); err != nil {
			return err
		}
		if err := writeAtomic(path+".pub", []byte(sshPublicKey(pub)+"\n"), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func validSSHPrivateKey(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	key, err := gossh.ParseRawPrivateKey(data)
	if err != nil {
		return false
	}
	switch key.(type) {
	case ed25519.PrivateKey, *ed25519.PrivateKey:
		return true
	default:
		return false
	}
}

func sshPublicKey(key ed25519.PublicKey) string {
	name := []byte("ssh-ed25519")
	payload := make([]byte, 4+len(name)+4+len(key))
	binary.BigEndian.PutUint32(payload, uint32(len(name)))
	copy(payload[4:], name)
	offset := 4 + len(name)
	binary.BigEndian.PutUint32(payload[offset:], uint32(len(key)))
	copy(payload[offset+4:], key)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(payload) + " axern-local"
}

func loadMetadata(path string) (Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, err
	}
	var value Metadata
	if err := json.Unmarshal(data, &value); err != nil {
		return Metadata{}, fmt.Errorf("parse local metadata: %w", err)
	}
	return value, nil
}

func saveMetadata(path string, value Metadata) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'), 0o600)
}
