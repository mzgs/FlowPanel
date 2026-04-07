package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"flowpanel/internal/app"

	"github.com/creack/pty"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"go.uber.org/zap"
)

const (
	defaultTerminalCols = 120
	defaultTerminalRows = 36
)

type terminalClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

type terminalServerMessage struct {
	Type    string `json:"type"`
	Cwd     string `json:"cwd,omitempty"`
	Message string `json:"message,omitempty"`
	PID     int    `json:"pid,omitempty"`
	Shell   string `json:"shell,omitempty"`
}

type terminalSocketWriter struct {
	conn net.Conn
	mu   sync.Mutex
}

type terminalRuntimeEvent struct {
	kind     string
	err      error
	exitCode int
}

func newTerminalWebSocketHandler(app *app.App) stdhttp.Handler {
	logger := app.Logger.Named("terminal")

	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			logger.Warn("upgrade terminal websocket failed", zap.Error(err))
			return
		}
		defer conn.Close()

		socket := &terminalSocketWriter{conn: conn}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		ptyFile, cmd, cwd, shell, err := startTerminalSession(ctx)
		if err != nil {
			logger.Error("start terminal session failed", zap.Error(err))
			_ = socket.writeJSON(terminalServerMessage{
				Type:    "error",
				Message: "Failed to start the local shell session.",
			})
			return
		}
		defer ptyFile.Close()

		if err := socket.writeJSON(terminalServerMessage{
			Type:  "ready",
			Cwd:   cwd,
			PID:   cmd.Process.Pid,
			Shell: shell,
		}); err != nil {
			logger.Debug("write terminal ready event failed", zap.Error(err))
			return
		}

		events := make(chan terminalRuntimeEvent, 3)

		go pumpTerminalOutput(socket, ptyFile, events)
		go handleTerminalClientMessages(conn, ptyFile, events)
		go waitForTerminalExit(cmd, events)

		event := <-events
		cancel()

		if event.kind == "process" {
			if event.err != nil && !errors.Is(event.err, context.Canceled) {
				logger.Debug("terminal process exited with error", zap.Error(event.err))
			}

			_ = socket.writeJSON(terminalServerMessage{
				Type:    "exit",
				Message: fmt.Sprintf("Shell exited with code %d.", event.exitCode),
			})
		} else if event.err != nil && !isTerminalDisconnectError(event.err) {
			logger.Debug("terminal session closed", zap.String("kind", event.kind), zap.Error(event.err))
		}
	})
}

func (w *terminalSocketWriter) writeBinary(payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return wsutil.WriteServerBinary(w.conn, payload)
}

func (w *terminalSocketWriter) writeJSON(payload terminalServerMessage) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	return wsutil.WriteServerText(w.conn, data)
}

func startTerminalSession(ctx context.Context) (*os.File, *exec.Cmd, string, string, error) {
	shell, args, err := resolveTerminalShell()
	if err != nil {
		return nil, nil, "", "", err
	}

	cwd, err := terminalStartDirectory()
	if err != nil {
		return nil, nil, "", "", err
	}

	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"TERM_PROGRAM=FlowPanel",
	)

	ptyFile, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: defaultTerminalCols,
		Rows: defaultTerminalRows,
	})
	if err != nil {
		return nil, nil, "", "", err
	}

	return ptyFile, cmd, cwd, filepath.Base(shell), nil
}

func resolveTerminalShell() (string, []string, error) {
	if runtime.GOOS == "windows" {
		for _, candidate := range []string{"powershell.exe", "pwsh.exe", "cmd.exe"} {
			path, err := exec.LookPath(candidate)
			if err == nil {
				if filepath.Base(path) == "cmd.exe" {
					return path, nil, nil
				}
				return path, []string{"-NoLogo"}, nil
			}
		}

		return "", nil, errors.New("no supported shell found")
	}

	candidates := []string{}
	if shell := os.Getenv("SHELL"); shell != "" {
		candidates = append(candidates, shell)
	}
	candidates = append(candidates, "/bin/zsh", "/bin/bash", "/bin/sh")

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}

		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}

		base := filepath.Base(path)
		switch base {
		case "zsh", "bash":
			return path, []string{"-il"}, nil
		default:
			return path, []string{"-i"}, nil
		}
	}

	return "", nil, errors.New("no supported shell found")
}

func terminalStartDirectory() (string, error) {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	return cwd, nil
}

func pumpTerminalOutput(socket *terminalSocketWriter, ptyFile *os.File, events chan<- terminalRuntimeEvent) {
	buffer := make([]byte, 4096)

	for {
		n, err := ptyFile.Read(buffer)
		if n > 0 {
			chunk := append([]byte(nil), buffer[:n]...)
			if writeErr := socket.writeBinary(chunk); writeErr != nil {
				events <- terminalRuntimeEvent{kind: "output", err: writeErr}
				return
			}
		}

		if err != nil {
			events <- terminalRuntimeEvent{kind: "output", err: err}
			return
		}
	}
}

func handleTerminalClientMessages(conn net.Conn, ptyFile *os.File, events chan<- terminalRuntimeEvent) {
	for {
		payload, op, err := wsutil.ReadClientData(conn)
		if err != nil {
			events <- terminalRuntimeEvent{kind: "input", err: err}
			return
		}

		switch op {
		case ws.OpClose:
			events <- terminalRuntimeEvent{kind: "input", err: io.EOF}
			return
		case ws.OpText:
			var message terminalClientMessage
			if err := json.Unmarshal(payload, &message); err != nil {
				events <- terminalRuntimeEvent{kind: "input", err: err}
				return
			}

			switch message.Type {
			case "input":
				if message.Data == "" {
					continue
				}
				if _, err := io.WriteString(ptyFile, message.Data); err != nil {
					events <- terminalRuntimeEvent{kind: "input", err: err}
					return
				}
			case "resize":
				cols, rows := clampTerminalSize(message.Cols, message.Rows)
				if err := pty.Setsize(ptyFile, &pty.Winsize{
					Cols: uint16(cols),
					Rows: uint16(rows),
				}); err != nil {
					events <- terminalRuntimeEvent{kind: "input", err: err}
					return
				}
			}
		}
	}
}

func waitForTerminalExit(cmd *exec.Cmd, events chan<- terminalRuntimeEvent) {
	err := cmd.Wait()
	if err == nil {
		events <- terminalRuntimeEvent{kind: "process", exitCode: 0}
		return
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		events <- terminalRuntimeEvent{kind: "process", err: err, exitCode: exitErr.ExitCode()}
		return
	}

	events <- terminalRuntimeEvent{kind: "process", err: err, exitCode: 1}
}

func clampTerminalSize(cols, rows int) (int, int) {
	if cols < 20 {
		cols = 20
	}
	if rows < 8 {
		rows = 8
	}

	return cols, rows
}

func isTerminalDisconnectError(err error) bool {
	return err == nil ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, context.Canceled)
}
