//go:build darwin

package server

import (
	"log"

	"github.com/getlantern/systray"
)

// macOS requires all AppKit work to stay on the process main thread. Both the
// systray and webview packages own an NSApplication run loop and delegate, so
// running them together can corrupt that shared state. Keep the menu-bar app
// native and open the management UI in the user's default browser instead.
func runTrayApp(rawURL string, activation <-chan struct{}, shutdown func()) {
	openDashboard := func() {
		if err := openBrowser(rawURL); err != nil {
			log.Printf("open management page: %v", err)
		}
	}

	if activation != nil {
		go func() {
			for range activation {
				openDashboard()
			}
		}()
	}

	systray.Run(func() {
		systray.SetIcon(appIcon)
		systray.SetTitle(appDisplayName)
		systray.SetTooltip(appDisplayName + " 正在运行")
		mOpen := systray.AddMenuItem("打开管理页面", "Open "+appDisplayName)
		mQuit := systray.AddMenuItem("退出", "Exit "+appDisplayName)
		go func() {
			openDashboard()
			for {
				select {
				case <-mOpen.ClickedCh:
					openDashboard()
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}, shutdown)
}
