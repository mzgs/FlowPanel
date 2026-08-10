//go:build !windows

package backup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func (s *Service) acquireOperationLock() (io.Closer, error) {
	if err := s.ensureBackupPath(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(s.backupPath, ".flowpanel-operation.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open backup operation lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrOperationActive
		}
		return nil, fmt.Errorf("lock backup operation: %w", err)
	}
	return file, nil
}
