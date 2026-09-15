package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeArgsExpandsExecInteractiveShortFlags(t *testing.T) {
	args := []string{"axctl", "allocation", "exec", "-it", "allocation-id", "--", "/bin/sh"}

	normalized := normalizeArgs(args)

	assert.Equal(t, []string{"axctl", "allocation", "exec", "-i", "-t", "allocation-id", "--", "/bin/sh"}, normalized)
}

func TestNormalizeArgsExpandsExecInteractiveShortFlagsInEitherOrder(t *testing.T) {
	args := []string{"axctl", "allocation", "exec", "-ti", "allocation-id", "--", "/bin/sh"}

	normalized := normalizeArgs(args)

	assert.Equal(t, []string{"axctl", "allocation", "exec", "-t", "-i", "allocation-id", "--", "/bin/sh"}, normalized)
}

func TestNormalizeArgsPreservesNonExecBundles(t *testing.T) {
	args := []string{"axctl", "--timeout", "45s", "allocation", "exec", "-it", "allocation-id", "--", "/bin/sh", "-it"}

	normalized := normalizeArgs(args)

	assert.Equal(t, []string{"axctl", "--timeout", "45s", "allocation", "exec", "-i", "-t", "allocation-id", "--", "/bin/sh", "-it"}, normalized)
}

func TestNormalizeArgsDoesNotRecognizeRemovedSandboxNoun(t *testing.T) {
	args := []string{"axctl", "sandbox", "exec", "-it", "allocation-id", "--", "/bin/sh"}

	normalized := normalizeArgs(args)

	assert.Equal(t, args, normalized)
}

func TestExpandExecShortFlagsRejectsUnknownFlags(t *testing.T) {
	expanded, ok := expandExecShortFlags("-ix")

	assert.False(t, ok)
	assert.Nil(t, expanded)
}
