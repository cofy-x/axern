package execflow

import (
	"os"
	"path/filepath"
	"testing"

	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
)

func TestExecProcessFromSpecOverridesArgsAndTerminal(t *testing.T) {
	source := &spec.Spec{
		Process: &spec.Process{
			Terminal: false,
			Cwd:      "/workspace",
			Env:      []string{"A=1"},
			User: spec.User{
				UID: 1000,
				GID: 1001,
			},
			Capabilities: &spec.LinuxCapabilities{
				Bounding: []string{"CAP_NET_RAW"},
			},
			Rlimits: []spec.POSIXRlimit{{
				Type: "RLIMIT_NOFILE",
				Hard: 64,
				Soft: 64,
			}},
		},
	}

	process, err := ExecProcessFromSpec(source, []string{"/bin/sh", "-c", "echo ok"}, nil, "", "", true)
	assert.NoError(t, err)
	if assert.NotNil(t, process) {
		assert.Equal(t, []string{"/bin/sh", "-c", "echo ok"}, process.Args)
		assert.True(t, process.Terminal)
		assert.Equal(t, "/workspace", process.Cwd)
		assert.Equal(t, []string{"A=1"}, process.Env)
		assert.EqualValues(t, 1000, process.User.UID)
		if assert.NotNil(t, process.Capabilities) {
			assert.Equal(t, []string{"CAP_NET_RAW"}, process.Capabilities.Bounding)
		}
		assert.Len(t, process.Rlimits, 1)
	}
	assert.Equal(t, []string{"A=1"}, source.Process.Env)
	assert.False(t, source.Process.Terminal)
}

func TestExecProcessFromSpecOverridesUserByName(t *testing.T) {
	rootfs := writeUserDatabase(t)
	source := &spec.Spec{
		Root: &spec.Root{Path: rootfs},
		Process: &spec.Process{
			Args: []string{"/bin/sh"},
			Env:  []string{"HOME=/root", "PATH=/usr/bin"},
			User: spec.User{UID: 0, GID: 0},
		},
	}

	process, err := ExecProcessFromSpec(source, []string{"/bin/sh"}, nil, "", "axern:developers", true)
	assert.NoError(t, err)
	if assert.NotNil(t, process) {
		assert.EqualValues(t, 1000, process.User.UID)
		assert.EqualValues(t, 2000, process.User.GID)
		assert.Equal(t, "axern", process.User.Username)
		assert.Contains(t, process.Env, "HOME=/home/axern")
		assert.Contains(t, process.Env, "USER=axern")
		assert.Contains(t, process.Env, "LOGNAME=axern")
		assert.Contains(t, process.Env, "SHELL=/bin/bash")
		assert.Contains(t, process.Env, "PATH=/usr/bin")
		assert.NotContains(t, process.Env, "HOME=/root")
	}
}

func TestExecProcessFromSpecRequestEnvOverridesUserDefaults(t *testing.T) {
	rootfs := writeUserDatabase(t)
	source := &spec.Spec{
		Root: &spec.Root{Path: rootfs},
		Process: &spec.Process{
			Args: []string{"/bin/sh"},
			Env:  []string{"HOME=/root"},
			User: spec.User{UID: 0, GID: 0},
		},
	}

	process, err := ExecProcessFromSpec(
		source,
		[]string{"/bin/sh"},
		map[string]string{"HOME": "/tmp/custom-home", "USER": "custom"},
		"",
		"axern",
		true,
	)
	assert.NoError(t, err)
	if assert.NotNil(t, process) {
		assert.EqualValues(t, 1000, process.User.UID)
		assert.EqualValues(t, 1000, process.User.GID)
		assert.Contains(t, process.Env, "HOME=/tmp/custom-home")
		assert.Contains(t, process.Env, "USER=custom")
		assert.Contains(t, process.Env, "LOGNAME=axern")
		assert.NotContains(t, process.Env, "HOME=/home/axern")
		assert.NotContains(t, process.Env, "HOME=/root")
	}
}

func TestExecProcessFromSpecOverridesUserByID(t *testing.T) {
	source := &spec.Spec{
		Process: &spec.Process{
			Args: []string{"/bin/sh"},
			User: spec.User{UID: 0, GID: 0},
		},
	}

	process, err := ExecProcessFromSpec(source, []string{"/bin/sh"}, nil, "", "1001:1002", true)
	assert.NoError(t, err)
	if assert.NotNil(t, process) {
		assert.EqualValues(t, 1001, process.User.UID)
		assert.EqualValues(t, 1002, process.User.GID)
		assert.Empty(t, process.User.Username)
	}
}

func TestExecProcessFromSpecNumericUserUsesPasswdPrimaryGroupWhenAvailable(t *testing.T) {
	rootfs := writeUserDatabase(t)
	source := &spec.Spec{
		Root:    &spec.Root{Path: rootfs},
		Process: &spec.Process{Args: []string{"/bin/sh"}},
	}

	process, err := ExecProcessFromSpec(source, []string{"/bin/sh"}, nil, "", "1000", true)
	assert.NoError(t, err)
	if assert.NotNil(t, process) {
		assert.EqualValues(t, 1000, process.User.UID)
		assert.EqualValues(t, 1000, process.User.GID)
	}
}

func TestExecProcessFromSpecRejectsUnknownUser(t *testing.T) {
	rootfs := writeUserDatabase(t)
	source := &spec.Spec{
		Root:    &spec.Root{Path: rootfs},
		Process: &spec.Process{Args: []string{"/bin/sh"}},
	}

	_, err := ExecProcessFromSpec(source, []string{"/bin/sh"}, nil, "", "missing", true)
	assert.Error(t, err)
}

func writeUserDatabase(t *testing.T) string {
	t.Helper()
	rootfs := t.TempDir()
	etc := filepath.Join(rootfs, "etc")
	if err := os.MkdirAll(etc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etc, "passwd"), []byte("root:x:0:0:root:/root:/bin/sh\naxern:x:1000:1000:Axern:/home/axern:/bin/bash\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etc, "group"), []byte("root:x:0:\naxern:x:1000:\ndevelopers:x:2000:axern\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return rootfs
}
