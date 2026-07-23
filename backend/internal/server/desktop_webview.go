//go:build !darwin

package server

import (
	"runtime"

	"github.com/getlantern/systray"
	webview "github.com/webview/webview_go"
)

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
