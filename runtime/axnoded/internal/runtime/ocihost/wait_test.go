package ocihost

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommonWaitParsesExitStatus(t *testing.T) {
	common, err := New(Config{Root: t.TempDir(), RuntimeName: "runc", RuntimeBinary: "runc"})
	require.NoError(t, err)
	common.SetExecutor(&mockExecutor{
		SuccessMap: map[string]bool{
			"runc --root": true,
		},
		OutputMap: map[string]string{
			"runc --root": `{"exitStatus":7}`,
		},
	})

	exit, ok, err := common.Wait(context.Background(), "wait", "axctl-test")

	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 7, exit.Status)
	assert.False(t, exit.Timestamp.IsZero())
}

func TestCommonWaitReportsRuntimeCommandFailureAsNoStatus(t *testing.T) {
	common, err := New(Config{Root: t.TempDir(), RuntimeName: "runc", RuntimeBinary: "runc"})
	require.NoError(t, err)
	common.SetExecutor(&mockExecutor{})

	exit, ok, err := common.Wait(context.Background(), "wait", "axctl-test")

	assert.Error(t, err)
	assert.False(t, ok)
	assert.Zero(t, exit.Status)
	assert.True(t, exit.Timestamp.IsZero())
}

func TestCommonWaitReportsParseFailureAsStatusUnavailable(t *testing.T) {
	common, err := New(Config{Root: t.TempDir(), RuntimeName: "runc", RuntimeBinary: "runc"})
	require.NoError(t, err)
	common.SetExecutor(&mockExecutor{
		SuccessMap: map[string]bool{
			"runc --root": true,
		},
		OutputMap: map[string]string{
			"runc --root": "not-an-exit-code",
		},
	})

	exit, ok, err := common.Wait(context.Background(), "wait", "axctl-test")

	assert.Error(t, err)
	assert.True(t, ok)
	assert.Zero(t, exit.Status)
	assert.True(t, exit.Timestamp.IsZero())
}
