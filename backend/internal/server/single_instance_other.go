//go:build !windows

package server

func acquireDesktopInstance(chan<- struct{}) (bool, func(), error) {
	return true, func() {}, nil
}
