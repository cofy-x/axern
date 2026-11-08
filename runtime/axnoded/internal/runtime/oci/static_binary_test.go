package oci

import (
	"encoding/binary"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// staticSandboxdFixture is a metadata-only ELF envelope, never a qualified runtime.
func staticSandboxdFixture() []byte {
	data := make([]byte, 64)
	copy(data, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(data[16:], 2)
	machine := uint16(62)
	if runtime.GOARCH == "arm64" {
		machine = 183
	}
	binary.LittleEndian.PutUint16(data[18:], machine)
	binary.LittleEndian.PutUint32(data[20:], 1)
	binary.LittleEndian.PutUint16(data[52:], 64)
	binary.LittleEndian.PutUint16(data[54:], 56)
	binary.LittleEndian.PutUint16(data[58:], 64)
	return data
}

func TestSandboxdInjectionRequiresCopiedStaticELF(t *testing.T) {
	for _, variant := range []string{"static", "script", "interpreter", "dynamic-without-sections", "wrong-arch"} {
		t.Run(variant, func(t *testing.T) {
			data := staticSandboxdFixture()
			switch variant {
			case "script":
				data = []byte("#!/bin/sh\nexit 0\n")
			case "interpreter", "dynamic-without-sections":
				data = append(data, make([]byte, 56)...)
				binary.LittleEndian.PutUint64(data[32:], 64)
				binary.LittleEndian.PutUint16(data[56:], 1)
				kind := uint32(3)
				if variant == "dynamic-without-sections" {
					kind = 2
				}
				binary.LittleEndian.PutUint32(data[64:], kind)
			case "wrong-arch":
				machine := uint16(183)
				if runtime.GOARCH == "arm64" {
					machine = 62
				}
				binary.LittleEndian.PutUint16(data[18:], machine)
			}
			root := t.TempDir()
			source, target := filepath.Join(root, "source"), filepath.Join(root, "injected")
			require.NoError(t, os.WriteFile(source, data, 0755))
			err := copySandboxdBinary(target, source)
			if variant == "static" {
				require.NoError(t, err)
				actual, err := os.ReadFile(target)
				require.NoError(t, err)
				require.Equal(t, data, actual)
				info, err := os.Stat(target)
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0555), info.Mode().Perm())
			} else {
				require.Error(t, err)
				require.NoFileExists(t, target, "rejected helper must not become an executable bundle member")
			}
		})
	}
}
