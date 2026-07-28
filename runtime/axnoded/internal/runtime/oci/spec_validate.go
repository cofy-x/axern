package oci

import (
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

func validateProcessArgs(ociSpec *spec.Spec) error {
	if ociSpec == nil || ociSpec.Process == nil || len(ociSpec.Process.Args) == 0 {
		logrus.Debug("invalid OCI process: command is empty")
		return errord.ErrInvalidArgument
	}
	if strings.TrimSpace(ociSpec.Process.Args[0]) == "" {
		logrus.Debug("invalid OCI process: command argv[0] is empty")
		return errord.ErrInvalidArgument
	}
	return nil
}
