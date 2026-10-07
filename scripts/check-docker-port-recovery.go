//go:build ignore

// Run with: go run scripts/check-docker-port-recovery.go
// Uses a fake Docker CLI and kills only temporary listeners created by this check.
package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"flowpanel/internal/dockercontainer"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "listen" {
		if os.Args[2] == "tcp" {
			tcp, tcpErr := net.Listen("tcp", "127.0.0.1:0")
			must(tcpErr)
			fmt.Println(tcp.Addr())
			for {
				connection, err := tcp.Accept()
				must(err)
				connection.Close()
			}
		}
		listener, err := net.ListenPacket("udp", "127.0.0.1:0")
		must(err)
		fmt.Println(listener.LocalAddr())
		for {
			_, _, err := listener.ReadFrom(make([]byte, 1))
			must(err)
		}
	}
	directory, err := os.MkdirTemp("", "flowpanel-port-check-")
	must(err)
	defer os.RemoveAll(directory)
	must(os.WriteFile(filepath.Join(directory, "docker"), []byte(`#!/bin/sh
case "$1" in
  context) echo unix:///var/run/docker.sock; exit 0 ;;
  ps) exit 0 ;;
esac
echo "$1" >> "$CHECK_CALLS"
if [ -f "$CHECK_STATE" ] && [ "$CHECK_REPEAT" != true ]; then exit 0; fi
touch "$CHECK_STATE"
echo "$CHECK_ERROR" >&2
exit 1
`), 0700))
	must(os.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH")))
	must(os.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock"))
	must(os.Unsetenv("DOCKER_CONTEXT"))
	executable, err := os.Executable()
	must(err)
	for _, scenario := range []struct {
		action, protocol, message string
		repeat, wantError         bool
	}{
		{"start", "tcp", "failed to bind host port %s/tcp: address already in use", false, false},
		{"restart", "udp", "failed to bind host port %s/udp: address already in use", false, false},
		{"restart", "tcp", "listen tcp %s: bind: address already in use", false, false},
		{"start", "tcp", "failed to bind host port %s/tcp: address already in use", true, true},
		{"start", "tcp", "failed to bind host port %s/tcp: permission denied", false, true},
		{"stop", "tcp", "failed to bind host port %s/tcp: address already in use", false, true},
	} {
		child := exec.Command(executable, "listen", scenario.protocol)
		stdout, err := child.StdoutPipe()
		must(err)
		must(child.Start())
		defer child.Process.Kill()
		scanner := bufio.NewScanner(stdout)
		if !scanner.Scan() {
			panic("listener did not start")
		}
		address := scanner.Text()
		state, calls := filepath.Join(directory, "state"), filepath.Join(directory, "calls")
		os.Remove(state)
		os.Remove(calls)
		must(os.Setenv("CHECK_STATE", state))
		must(os.Setenv("CHECK_CALLS", calls))
		must(os.Setenv("CHECK_REPEAT", strconv.FormatBool(scenario.repeat)))
		must(os.Setenv("CHECK_ERROR", fmt.Sprintf(scenario.message, address)))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		actionErr := dockercontainer.RunAction(ctx, "temporary-check-container", scenario.action)
		cancel()
		var available bool
		if scenario.protocol == "tcp" {
			listener, err := net.Listen("tcp", address)
			available = err == nil
			if available {
				listener.Close()
			}
		} else {
			listener, err := net.ListenPacket("udp", address)
			available = err == nil
			if available {
				listener.Close()
			}
		}
		child.Process.Kill()
		child.Wait()
		if (actionErr != nil) != scenario.wantError {
			panic(fmt.Sprintf("unexpected result: %v", actionErr))
		}
		log, err := os.ReadFile(calls)
		must(err)
		wantCalls := scenario.action + "\n"
		if !scenario.wantError || scenario.repeat {
			wantCalls += "start\n"
		}
		if string(log) != wantCalls {
			panic("unexpected Docker calls: " + string(log))
		}
		if available != (!scenario.wantError || scenario.repeat) {
			panic("unexpected listener state")
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	defer listener.Close()
	must(os.Setenv("CHECK_ERROR", fmt.Sprintf("failed to bind host port %s/tcp: address already in use", listener.Addr())))
	for _, endpoint := range []string{"unix:///var/run/docker.sock", "ssh://remote.example"} {
		os.Remove(filepath.Join(directory, "state"))
		must(os.Setenv("DOCKER_HOST", endpoint))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := dockercontainer.RunAction(ctx, "temporary-check-container", "start")
		cancel()
		if err == nil || !(strings.Contains(err.Error(), "protected") || strings.Contains(err.Error(), "local Docker")) {
			panic(fmt.Sprintf("listener protection failed: %v", err))
		}
	}
	fmt.Println("PASS: TCP/UDP recovery, restart-to-start, bounded retry, unrelated errors, stop and listener protection")
}
