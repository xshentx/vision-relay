//go:build windows

package server

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestReplaceFileSafelyRewritesDestinationHeldWithoutDeleteSharing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "session.jsonl")
	tmp := filepath.Join(dir, "replacement.tmp")
	original := []byte("original session contents\n")
	replacement := []byte("cleaned session contents\n")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, replacement, 0o600); err != nil {
		t.Fatal(err)
	}

	// Go opens Windows files with FILE_SHARE_READ|FILE_SHARE_WRITE but without
	// FILE_SHARE_DELETE, matching the handle pattern that caused the UI failure.
	held, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	if err := replaceFileSafely(tmp, target); err != nil {
		t.Fatalf("replace while destination is open: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, replacement) {
		t.Fatalf("destination = %q, want %q", got, replacement)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf("replacement temporary file still exists: %v", err)
	}
}

func TestReplaceFileSafelyDoesNotDamageWriteLockedDestination(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "session.jsonl")
	tmp := filepath.Join(dir, "replacement.tmp")
	original := []byte("original must survive\n")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("replacement\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	targetUTF16, err := windows.UTF16PtrFromString(target)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(
		targetUTF16,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)

	if err := replaceFileSafely(tmp, target); err == nil {
		t.Fatal("replace unexpectedly succeeded while destination denied write and delete sharing")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("failed replacement damaged destination: got %q want %q", got, original)
	}
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("failed replacement removed recovery temporary file: %v", err)
	}
}
