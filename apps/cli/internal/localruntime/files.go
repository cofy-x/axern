package localruntime

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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
	if validCertificateSet(dir) {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "axern-local-ca"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err := writePrivateKey(filepath.Join(dir, "ca.key"), caKey); err != nil {
		return err
	}
	if err := writeCertificate(filepath.Join(dir, "ca.crt"), caDER); err != nil {
		return err
	}
	dnsNames := []string{"localhost", "host.docker.internal", "controld", "tunneld", "gatewayd", "registry"}
	serverNames := []string{"controld", "gatewayd", "tunneld"}
	for index, name := range serverNames {
		usages := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		if name != "controld" {
			usages = append(usages, x509.ExtKeyUsageClientAuth)
		}
		if err := issueCertificate(dir, name, int64(index+2), caTemplate, caKey, dnsNames, usages); err != nil {
			return err
		}
	}
	clients := []string{"client", "node", "rollout-worker"}
	commonNames := []string{"axern-local-client", "axern-node", "rollout-worker"}
	for index, name := range clients {
		if err := issueClientCertificate(dir, name, commonNames[index], int64(index+10), caTemplate, caKey); err != nil {
			return err
		}
	}
	return nil
}

func validCertificateSet(dir string) bool {
	for _, name := range []string{"ca.crt", "controld.crt", "gatewayd.crt", "tunneld.crt", "client.crt", "node.crt", "rollout-worker.crt"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return false
		}
		block, _ := pem.Decode(data)
		if block == nil {
			return false
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || time.Until(cert.NotAfter) < 30*24*time.Hour {
			return false
		}
	}
	for _, name := range []string{"controld", "gatewayd", "tunneld", "client", "node", "rollout-worker"} {
		if _, err := tls.LoadX509KeyPair(filepath.Join(dir, name+".crt"), filepath.Join(dir, name+".key")); err != nil {
			return false
		}
	}
	caKeyData, err := os.ReadFile(filepath.Join(dir, "ca.key"))
	if err != nil {
		return false
	}
	caKeyBlock, _ := pem.Decode(caKeyData)
	if caKeyBlock == nil {
		return false
	}
	if _, err := x509.ParsePKCS1PrivateKey(caKeyBlock.Bytes); err != nil {
		key, parseErr := x509.ParsePKCS8PrivateKey(caKeyBlock.Bytes)
		if parseErr != nil {
			return false
		}
		if _, ok := key.(*rsa.PrivateKey); !ok {
			return false
		}
	}
	return true
}

func issueCertificate(dir, name string, serial int64, ca *x509.Certificate, caKey *rsa.PrivateKey, dns []string, usages []x509.ExtKeyUsage) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: usages, DNSNames: append(append([]string(nil), dns...), name), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err := writePrivateKey(filepath.Join(dir, name+".key"), key); err != nil {
		return err
	}
	return writeCertificate(filepath.Join(dir, name+".crt"), der)
}

func issueClientCertificate(dir, fileName, commonName string, serial int64, ca *x509.Certificate, caKey *rsa.PrivateKey) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	template := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: commonName}, NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	if err := writePrivateKey(filepath.Join(dir, fileName+".key"), key); err != nil {
		return err
	}
	return writeCertificate(filepath.Join(dir, fileName+".crt"), der)
}

func writePrivateKey(path string, key *rsa.PrivateKey) error {
	return writeAtomic(path, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600)
}

func writeCertificate(path string, der []byte) error {
	return writeAtomic(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
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
	clientPub, err := os.ReadFile(filepath.Join(dir, "gateway_client_ed25519.pub"))
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, "authorized_keys"), clientPub, 0o600)
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
