package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeArgsExpandsExecInteractiveShortFlags(t *testing.T) {
	args := []string{"axctl", "sandbox", "exec", "-it", "sandbox-id", "--", "/bin/sh"}

	normalized := normalizeArgs(args)

	assert.Equal(t, []string{"axctl", "sandbox", "exec", "-i", "-t", "sandbox-id", "--", "/bin/sh"}, normalized)
}

func TestNormalizeArgsExpandsExecInteractiveShortFlagsInEitherOrder(t *testing.T) {
	args := []string{"axctl", "sandbox", "exec", "-ti", "sandbox-id", "--", "/bin/sh"}

	normalized := normalizeArgs(args)

	assert.Equal(t, []string{"axctl", "sandbox", "exec", "-t", "-i", "sandbox-id", "--", "/bin/sh"}, normalized)
}

func TestNormalizeArgsPreservesNonExecBundles(t *testing.T) {
	args := []string{"axctl", "--timeout", "45s", "sandbox", "exec", "-it", "sandbox-id", "--", "/bin/sh", "-it"}

	normalized := normalizeArgs(args)

	assert.Equal(t, []string{"axctl", "--timeout", "45s", "sandbox", "exec", "-i", "-t", "sandbox-id", "--", "/bin/sh", "-it"}, normalized)
}

func TestExpandExecShortFlagsRejectsUnknownFlags(t *testing.T) {
	expanded, ok := expandExecShortFlags("-ix")

	assert.False(t, ok)
	assert.Nil(t, expanded)
}
