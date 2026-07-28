package ocihost

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockExecutor struct {
	SuccessMap map[string]bool
	OutputMap  map[string]string

	lastCommand string
	lastArgs    []string
}

func (m *mockExecutor) Execute(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	_ = ctx
	m.lastCommand = cmd
	m.lastArgs = append([]string(nil), args...)
	key := strings.Join(append([]string{cmd}, args...), " ")
	if m.getSuccess(key) {
		return m.getOutput(key), nil
	}
	return m.getOutput(key), fmt.Errorf("mock error")
}

func (m *mockExecutor) getSuccess(cmd string) bool {
	if m.SuccessMap == nil {
		return false
	}
	for key, success := range m.SuccessMap {
		if strings.Contains(cmd, key) {
			return success
		}
	}
	return false
}

func (m *mockExecutor) getOutput(cmd string) []byte {
	if m.OutputMap == nil {
		return []byte("")
	}
	for key, output := range m.OutputMap {
		if strings.Contains(cmd, key) {
			return []byte(output)
		}
	}
	return []byte("")
}

func TestCommonRunUsesConfiguredExecutor(t *testing.T) {
	executor := &mockExecutor{
		SuccessMap: map[string]bool{
			"runc --root": true,
		},
		OutputMap: map[string]string{
			"runc --root": "mock output",
		},
	}
	common, err := New(Config{Root: t.TempDir(), RuntimeName: "runc", RuntimeBinary: "runc"})
	require.NoError(t, err)
	common.SetExecutor(executor)

	output, err := common.Run(context.Background(), "list")

	require.NoError(t, err)
	assert.Equal(t, "mock output", string(output))
	assert.Equal(t, "runc", executor.lastCommand)
	assert.Equal(t, []string{"--root", common.runtimeRoot, "list"}, executor.lastArgs)
}

func TestCommonSetExecutorNilRestoresDefault(t *testing.T) {
	common, err := New(Config{Root: t.TempDir(), RuntimeName: "runc", RuntimeBinary: "runc"})
	require.NoError(t, err)
	common.SetExecutor(&mockExecutor{})

	common.SetExecutor(nil)

	assert.IsType(t, &SystemExecutor{}, common.executor)
}
