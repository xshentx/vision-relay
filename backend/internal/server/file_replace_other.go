//go:build !windows

package server

import "os"

// replaceFileSafely atomically installs tmpPath on platforms where rename can
// replace an existing destination even while normal readers have it open.
func replaceFileSafely(tmpPath, targetPath string) error {
	return os.Rename(tmpPath, targetPath)
}
