//go:build windows

package backup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

type windowsOperationLock struct {
	file       *os.File
	overlapped windows.Overlapped
}

func (l *windowsOperationLock) Close() error {
	unlockErr := windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &l.overlapped)
	return errors.Join(unlockErr, l.file.Close())
}

func (s *Service) acquireOperationLock() (io.Closer, error) {
	if err := s.ensureBackupPath(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(s.backupPath, ".flowpanel-operation.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open backup operation lock: %w", err)
	}
	lock := &windowsOperationLock{file: file}
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &lock.overlapped)
	if err == nil {
		return lock, nil
	}
	_ = file.Close()
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return nil, ErrOperationActive
	}
	return nil, fmt.Errorf("lock backup operation: %w", err)
}
