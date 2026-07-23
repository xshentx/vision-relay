package server

import (
	"os/exec"
	"runtime"
	"sync"
)

func openBrowser(rawURL string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	case "darwin":
		return exec.Command("open", rawURL).Start()
	default:
		return exec.Command("xdg-open", rawURL).Start()
	}
}

func finishClientWindowRun(runEnded, destroy func()) {
	// Clear the logical open state before destroying the WebView. An activation
	// arriving after the native run loop has stopped can then queue the next
	// window instead of trying to focus a vanished HWND.
	if runEnded != nil {
		runEnded()
	}
	destroy()
}

type clientWindowState struct {
	mu     sync.Mutex
	open   bool
	queued bool
}

func (s *clientWindowState) requestOpen() (focusExisting, enqueue bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open {
		return true, false
	}
	if s.queued {
		return false, false
	}
	s.queued = true
	return false, true
}

func (s *clientWindowState) beginOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queued = false
	if s.open {
		return false
	}
	s.open = true
	return true
}

func (s *clientWindowState) markClosed() {
	s.mu.Lock()
	s.open = false
	s.mu.Unlock()
}
