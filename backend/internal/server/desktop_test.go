package server

import "testing"

func TestClientWindowSizesAreInternallyConsistent(t *testing.T) {
	if clientWindowMinWidth != 960 || clientWindowMinHeight != 640 {
		t.Fatalf("minimum window size = %dx%d; want 960x640", clientWindowMinWidth, clientWindowMinHeight)
	}
	if clientWindowWidth < clientWindowMinWidth || clientWindowHeight < clientWindowMinHeight {
		t.Fatalf(
			"initial window size %dx%d is smaller than minimum %dx%d",
			clientWindowWidth,
			clientWindowHeight,
			clientWindowMinWidth,
			clientWindowMinHeight,
		)
	}
}

func TestClientWindowStateFocusesExistingWindowWithoutQueueingAnother(t *testing.T) {
	state := &clientWindowState{}

	focus, enqueue := state.requestOpen()
	if focus || !enqueue {
		t.Fatalf("first request = focus %t, enqueue %t; want false, true", focus, enqueue)
	}
	focus, enqueue = state.requestOpen()
	if focus || enqueue {
		t.Fatalf("queued request = focus %t, enqueue %t; want false, false", focus, enqueue)
	}
	if !state.beginOpen() {
		t.Fatal("queued window did not transition to open")
	}
	focus, enqueue = state.requestOpen()
	if !focus || enqueue {
		t.Fatalf("request with open window = focus %t, enqueue %t; want true, false", focus, enqueue)
	}

	state.markClosed()
	focus, enqueue = state.requestOpen()
	if focus || !enqueue {
		t.Fatalf("request after close = focus %t, enqueue %t; want false, true", focus, enqueue)
	}
}

func TestFinishClientWindowRunClearsStateBeforeDestroy(t *testing.T) {
	state := &clientWindowState{}
	if _, enqueue := state.requestOpen(); !enqueue {
		t.Fatal("initial window was not queued")
	}
	if !state.beginOpen() {
		t.Fatal("queued window did not transition to open")
	}

	finishClientWindowRun(state.markClosed, func() {
		// Model an activation in the interval that previously existed between
		// WebView destruction and markClosed.
		focus, enqueue := state.requestOpen()
		if focus || !enqueue {
			t.Fatalf("activation during destroy = focus %t, enqueue %t; want false, true", focus, enqueue)
		}
	})
}
