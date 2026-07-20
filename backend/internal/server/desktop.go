package server

import (
	"os/exec"
	"runtime"
	"sync"

	"github.com/getlantern/systray"
	webview "github.com/webview/webview_go"
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

func runClientWindow(rawURL string, runEnded func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	w := webview.New(false)
	defer finishClientWindowRun(runEnded, w.Destroy)
	w.SetTitle(appDisplayName)
	w.SetSize(1180, 820, webview.HintNone)
	w.Navigate(rawURL)
	w.Run()
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

func runTrayApp(rawURL string, activation <-chan struct{}, shutdown func()) {
	openCh := make(chan struct{}, 1)
	windowState := &clientWindowState{}

	openWindow := func() {
		focusExisting, enqueue := windowState.requestOpen()
		if focusExisting {
			focusClientWindow()
			return
		}
		if enqueue {
			openCh <- struct{}{}
		}
	}

	go func() {
		for range openCh {
			if !windowState.beginOpen() {
				focusClientWindow()
				continue
			}
			runClientWindow(rawURL, windowState.markClosed)
		}
	}()

	if activation != nil {
		go func() {
			for range activation {
				openWindow()
			}
		}()
	}

	systray.Run(func() {
		systray.SetIcon(appIcon)
		systray.SetTitle(appDisplayName)
		systray.SetTooltip(appDisplayName + " 正在运行")
		mOpen := systray.AddMenuItem("打开窗口", "Open "+appDisplayName)
		mQuit := systray.AddMenuItem("退出", "Exit "+appDisplayName)
		go func() {
			openWindow()
			for {
				select {
				case <-mOpen.ClickedCh:
					openWindow()
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}, func() {
		shutdown()
	})
}
