package executil

import (
	"os/exec"
	"sync"
)

const DefaultOutputLimit = 1 << 20

// TailBuffer keeps only the most recent bytes written while continuing to
// drain command output so a noisy subprocess cannot block or exhaust memory.
type TailBuffer struct {
	mu        sync.Mutex
	data      []byte
	limit     int
	truncated bool
}

func NewTailBuffer(limit int) *TailBuffer {
	if limit < 0 {
		limit = 0
	}
	return &TailBuffer{data: make([]byte, 0, limit), limit: limit}
}

func (b *TailBuffer) Write(payload []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(payload)
	if b.limit == 0 {
		b.truncated = b.truncated || written > 0
		return written, nil
	}
	if written >= b.limit {
		b.data = append(b.data[:0], payload[written-b.limit:]...)
		b.truncated = true
		return written, nil
	}
	if overflow := len(b.data) + written - b.limit; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
		b.truncated = true
	}
	b.data = append(b.data, payload...)
	return written, nil
}

func (b *TailBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...)
}

func (b *TailBuffer) String() string {
	return string(b.Bytes())
}

func (b *TailBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

func RunCombined(command *exec.Cmd, limit int) ([]byte, bool, error) {
	output := NewTailBuffer(limit)
	command.Stdout, command.Stderr = output, output
	err := command.Run()
	return output.Bytes(), output.Truncated(), err
}
