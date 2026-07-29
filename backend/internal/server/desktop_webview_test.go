//go:build !darwin

package server

import (
	"fmt"
	"reflect"
	"testing"

	webview "github.com/webview/webview_go"
)

type recordingClientWindow struct {
	calls []string
}

func (w *recordingClientWindow) record(call string) {
	w.calls = append(w.calls, call)
}

func (w *recordingClientWindow) SetTitle(title string) {
	w.record(fmt.Sprintf("SetTitle(%q)", title))
}

func (w *recordingClientWindow) SetSize(width, height int, hint webview.Hint) {
	w.record(fmt.Sprintf("SetSize(%d,%d,%d)", width, height, hint))
}

func (w *recordingClientWindow) Navigate(rawURL string) {
	w.record(fmt.Sprintf("Navigate(%q)", rawURL))
}

func (w *recordingClientWindow) Run() {
	w.record("Run")
}

func (w *recordingClientWindow) Destroy() {
	w.record("Destroy")
}

func TestRunClientWindowInstanceConfiguresMinimumSizeAndLifecycle(t *testing.T) {
	const rawURL = "http://127.0.0.1:8080/"
	w := &recordingClientWindow{}

	runClientWindowInstance(w, rawURL, func() {
		w.record("runEnded")
	})

	want := []string{
		fmt.Sprintf("SetTitle(%q)", appDisplayName),
		fmt.Sprintf("SetSize(%d,%d,%d)", clientWindowWidth, clientWindowHeight, webview.HintNone),
		fmt.Sprintf("SetSize(%d,%d,%d)", clientWindowMinWidth, clientWindowMinHeight, webview.HintMin),
		fmt.Sprintf("Navigate(%q)", rawURL),
		"Run",
		"runEnded",
		"Destroy",
	}
	if !reflect.DeepEqual(w.calls, want) {
		t.Fatalf("window calls = %#v; want %#v", w.calls, want)
	}
}
