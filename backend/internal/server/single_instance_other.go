//go:build !windows && !darwin

package server

func acquireDesktopInstance(chan<- struct{}) (bool, func(), error) {
	return true, func() {}, nil
}
