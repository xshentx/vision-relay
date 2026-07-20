package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDuplicateLaunchStopsBeforeCodexAndApplicationInitialization(t *testing.T) {
	codexDir := filepath.Join(t.TempDir(), ".codex")
	sessionDir := filepath.Join(codexDir, "sessions")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// These files represent the two protected Codex features: the official
	// login and the unified official/third-party session history.
	authPath := filepath.Join(codexDir, "auth.json")
	historyPath := filepath.Join(sessionDir, "session.jsonl")
	authBefore := []byte(`{"tokens":{"access_token":"official-login"}}`)
	historyBefore := []byte("{\"session\":\"official-and-third-party\"}\n")
	if err := os.WriteFile(authPath, authBefore, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(historyPath, historyBefore, 0o600); err != nil {
		t.Fatal(err)
	}

	startCalled := false
	releaseCalled := false
	runDesktopInstance(
		func(chan<- struct{}) (bool, func(), error) {
			return false, func() { releaseCalled = true }, nil
		},
		func(chan struct{}) {
			startCalled = true
			_ = os.WriteFile(authPath, []byte("changed"), 0o600)
			_ = os.WriteFile(historyPath, []byte("changed"), 0o600)
		},
	)

	if startCalled {
		t.Fatal("duplicate launch reached primary application initialization")
	}
	if releaseCalled {
		t.Fatal("duplicate launch tried to release a primary-instance handle")
	}
	assertFileBytes(t, authPath, authBefore)
	assertFileBytes(t, historyPath, historyBefore)
}

func TestDesktopInstanceAcquisitionFailureStopsBeforeInitialization(t *testing.T) {
	startCalled := false
	runDesktopInstance(
		func(chan<- struct{}) (bool, func(), error) {
			return false, nil, errors.New("mutex unavailable")
		},
		func(chan struct{}) { startCalled = true },
	)
	if startCalled {
		t.Fatal("failed single-instance guard reached primary application initialization")
	}
}

func TestPrimaryDesktopInstanceInitializesAndReleases(t *testing.T) {
	startCalled := false
	releaseCalled := false
	runDesktopInstance(
		func(chan<- struct{}) (bool, func(), error) {
			return true, func() { releaseCalled = true }, nil
		},
		func(chan struct{}) { startCalled = true },
	)
	if !startCalled {
		t.Fatal("primary instance did not initialize")
	}
	if !releaseCalled {
		t.Fatal("primary instance handle was not released")
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s changed during duplicate launch", path)
	}
}
