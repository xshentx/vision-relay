//go:build !windows

package server

import "errors"

func startUpdateHelper(_, _ string, _ int, _ []string) error {
	return errors.New("自动更新仅支持 Windows")
}
func RunUpdateHelperIfRequested() bool { return false }
func cleanupUpdateHelper()             {}
