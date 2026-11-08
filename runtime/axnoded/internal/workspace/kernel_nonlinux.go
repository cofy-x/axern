//go:build !linux

package workspace

import (
	"errors"
	"os"
)

type kernelPlatform struct{}

func (kernelPlatform) space(*os.File) (int64, int64, error) { return 0, 0, errPlatform }

func (kernelPlatform) guestDirectory(*os.File) error { return errPlatform }

var errPlatform = errors.New("persistent Workspaces require Linux XFS project quotas")

func (kernelPlatform) root(*os.File) (directoryIdentity, string, error) {
	return directoryIdentity{}, "", errPlatform
}
func (kernelPlatform) directory(*os.File) (directoryIdentity, error) {
	return directoryIdentity{}, errPlatform
}
func (kernelPlatform) quota(*os.File, uint32) (quota, error)                    { return quota{}, errPlatform }
func (kernelPlatform) assign(*os.File, uint32) error                            { return errPlatform }
func (kernelPlatform) limits(*os.File, uint32, uint64, uint64) error            { return errPlatform }
func (kernelPlatform) verify(*os.File, uint32) error                            { return errPlatform }
func (kernelPlatform) remove(*os.File, string, directoryIdentity, uint32) error { return errPlatform }
func (kernelPlatform) sync(*os.File) error                                      { return errPlatform }
func openDirectory(path string) (*os.File, error)                               { return os.Open(path) }
func openRegular(path string) (*os.File, error)                                 { return os.Open(path) }
func ensureMetadataDirectory(string) error                                      { return errPlatform }

func operatorReserveForFilesystem(*os.File, string, int64, int64) (int64, error) {
	return 0, errPlatform
}

func openChildDirectory(*os.File, string) (*os.File, error) { return nil, errPlatform }
