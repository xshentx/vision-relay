//go:build windows

package server

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

const windowsFileReplaceAttempts = 8

// replaceFileSafely prefers an atomic rename. Some clients keep their JSONL or
// configuration file open without FILE_SHARE_DELETE, which makes every rename
// fail on Windows even though the same handle still permits writes. For that
// specific sharing failure, rewrite the existing file through its stable path.
// The original bytes are retained in memory and restored if the rewrite fails.
func replaceFileSafely(tmpPath, targetPath string) error {
	renameErr := os.Rename(tmpPath, targetPath)
	if renameErr == nil {
		return nil
	}
	if !isWindowsFileSharingError(renameErr) {
		return renameErr
	}

	var rewriteErr error
	for attempt := 0; attempt < windowsFileReplaceAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 25 * time.Millisecond)
		}
		rewriteErr = rewriteOpenWindowsFile(tmpPath, targetPath)
		if rewriteErr == nil {
			if removeErr := os.Remove(tmpPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return fmt.Errorf("remove replacement temporary file: %w", removeErr)
			}
			return nil
		}
		if !isWindowsFileSharingError(rewriteErr) {
			break
		}
	}
	return fmt.Errorf("replace %s after Windows sharing failure: %w", targetPath, errors.Join(renameErr, rewriteErr))
}

func rewriteOpenWindowsFile(tmpPath, targetPath string) error {
	replacement, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("read replacement: %w", err)
	}
	original, err := os.ReadFile(targetPath)
	if err != nil {
		return fmt.Errorf("read original before rewrite: %w", err)
	}

	file, err := os.OpenFile(targetPath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open destination for in-place rewrite: %w", err)
	}
	writeErr := rewriteWindowsFileHandle(file, replacement)
	if writeErr != nil {
		rollbackErr := rewriteWindowsFileHandle(file, original)
		closeErr := file.Close()
		if rollbackErr != nil {
			return fmt.Errorf("rewrite destination: %w (rollback failed: %v; close: %v)", writeErr, rollbackErr, closeErr)
		}
		return fmt.Errorf("rewrite destination: %w (original restored; close: %v)", writeErr, closeErr)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close rewritten destination: %w", err)
	}
	return nil
}

func rewriteWindowsFileHandle(file *os.File, raw []byte) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if len(raw) > 0 {
		written, err := file.Write(raw)
		if err != nil {
			return err
		}
		if written != len(raw) {
			return io.ErrShortWrite
		}
	}
	return file.Sync()
}

func isWindowsFileSharingError(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION) ||
		errors.Is(err, windows.ERROR_USER_MAPPED_FILE)
}
