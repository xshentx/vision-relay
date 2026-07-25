//go:build !windows

package server

import "errors"

func installUpdate(_, _ string, _ []string) error {
	return errors.New("自动更新仅支持 Windows")
}
func cleanupUpdateFiles()        {}
func waitForUpdateParent() error { return nil }
