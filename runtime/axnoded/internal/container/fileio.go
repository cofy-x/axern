package container

import (
	"errors"
	"os"
	"strings"
	"sync"

	"github.com/cofy-x/axern/runtime/axnoded/pkg/fileutil"
)

var ioUtil IoUtil
var ioUtilOnce sync.Once

func Os() IoUtil {
	ioUtilOnce.Do(func() {
		if os.Getenv("TEST_UT") == "true" {
			ioUtil = &MockIoUtil{
				SuccessMap: map[string]bool{
					"success": true,
				},
			}
		} else {
			ioUtil = &EmbedIoUtil{}
		}
	})
	return ioUtil
}

type IoUtil interface {
	WriteFile(filename string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	Stat(name string) (os.FileInfo, error)
}

type EmbedIoUtil struct{}

func (e *EmbedIoUtil) WriteFile(filename string, data []byte, perm os.FileMode) error {
	return fileutil.AtomicWriteFile(filename, data, perm)
}

func (e *EmbedIoUtil) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (e *EmbedIoUtil) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

type MockIoUtil struct {
	SuccessMap map[string]bool
}

func (m *MockIoUtil) WriteFile(filename string, data []byte, perm os.FileMode) error {
	if m.SuccessMap == nil {
		return os.ErrNotExist
	}
	for key, success := range m.SuccessMap {
		if strings.Contains(string(data), key) && success {
			return nil
		}
	}
	return errors.New("mock error")
}

func (m *MockIoUtil) MkdirAll(path string, perm os.FileMode) error {
	return nil
}

func (m *MockIoUtil) Stat(name string) (os.FileInfo, error) {
	return nil, nil
}
