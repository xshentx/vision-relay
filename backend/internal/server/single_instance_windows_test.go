//go:build windows

package server

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDesktopInstanceObjectNamesAreGlobalAndUserScoped(t *testing.T) {
	mutexName, activationName, err := desktopInstanceObjectNames()
	if err != nil {
		t.Fatal(err)
	}
	for label, name := range map[string]string{
		"mutex":      mutexName,
		"activation": activationName,
	} {
		if !strings.HasPrefix(name, `Global\VisionRelay.S-`) {
			t.Fatalf("%s name %q is not global and user-SID scoped", label, name)
		}
	}
	if mutexName == activationName {
		t.Fatal("mutex and activation event names must differ")
	}
}

func TestDesktopSingleInstanceSignalsPrimaryAndRejectsDuplicate(t *testing.T) {
	baseMutexName, baseActivationName, err := desktopInstanceObjectNames()
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf(".Test.%d.%d", os.Getpid(), time.Now().UnixNano())
	mutexName := baseMutexName + suffix
	activationName := baseActivationName + suffix
	activation := make(chan struct{}, 1)

	primary, releasePrimary, err := acquireDesktopInstanceWithNames(mutexName, activationName, activation)
	if err != nil {
		t.Fatal(err)
	}
	if !primary {
		t.Fatal("first instance was not elected primary")
	}
	defer releasePrimary()

	duplicate, releaseDuplicate, err := acquireDesktopInstanceWithNames(mutexName, activationName, make(chan struct{}, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer releaseDuplicate()
	if duplicate {
		t.Fatal("second instance was incorrectly elected primary")
	}

	select {
	case <-activation:
	case <-time.After(2 * time.Second):
		t.Fatal("primary instance did not receive the activation signal")
	}
}

func TestDesktopSingleInstanceCanStartAgainAfterPrimaryReleases(t *testing.T) {
	baseMutexName, baseActivationName, err := desktopInstanceObjectNames()
	if err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf(".ReleaseTest.%d.%d", os.Getpid(), time.Now().UnixNano())
	mutexName := baseMutexName + suffix
	activationName := baseActivationName + suffix

	primary, release, err := acquireDesktopInstanceWithNames(mutexName, activationName, make(chan struct{}, 1))
	if err != nil || !primary {
		t.Fatalf("first acquire = primary %t, err %v", primary, err)
	}
	release()

	primary, release, err = acquireDesktopInstanceWithNames(mutexName, activationName, make(chan struct{}, 1))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if !primary {
		t.Fatal("instance lock remained held after primary released it")
	}
}
