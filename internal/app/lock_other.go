//go:build !unix

package app

import (
	"errors"
	"os"
)

// AcquireHostLock is the non-Unix placeholder. Only Linux is built and
// claimed for now; other platforms are added after a real build passes.
func AcquireHostLock(dataDir string) (*os.File, error) {
	return nil, errors.New("当前平台暂不支持宿主锁")
}
