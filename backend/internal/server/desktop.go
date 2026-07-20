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

func runClientWindow(rawURL string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle(appDisplayName)
	w.SetSize(1180, 820, webview.HintNone)
	w.Navigate(rawURL)
	w.Run()
}

func runTrayApp(rawURL string, shutdown func()) {
	openCh := make(chan struct{}, 1)
	var windowMu sync.Mutex
	windowOpen := false

	openWindow := func() {
		select {
		case openCh <- struct{}{}:
		default:
		}
	}

	go func() {
		for range openCh {
			windowMu.Lock()
			if windowOpen {
				windowMu.Unlock()
				continue
			}
			windowOpen = true
			windowMu.Unlock()

			runClientWindow(rawURL)

			windowMu.Lock()
			windowOpen = false
			windowMu.Unlock()
		}
	}()

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
