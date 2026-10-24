//go:build !windows

package a9

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// signalIsolate puts the command in its own process group, makes context
// cancellation (the kit's command deadline included) reap the command with
// everything it started while the parent relationship still identifies it, and
// bounds the wait for output handles a surviving descendant could hold open.
func signalIsolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if !signalReap(cmd.Process.Pid) {
			return errors.New("scenario processes survived cancellation")
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
}

type signalProc struct{ pid, ppid, pgid int }

// signalProcesses lists the live (non-zombie) processes of the host. A failed
// observation is an error: it never reads as an empty host.
func signalProcesses() ([]signalProc, error) {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,pgid=,stat=").Output()
	if err != nil {
		return nil, err
	}
	var procs []signalProc
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || strings.HasPrefix(f[3], "Z") {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		pgid, err3 := strconv.Atoi(f[2])
		if err1 == nil && err2 == nil && err3 == nil {
			procs = append(procs, signalProc{pid, ppid, pgid})
		}
	}
	return procs, nil
}

// signalGrow adds to members the processes below them or in a group they use,
// except the test's own. Members stay known after their parent dies, so
// orphans of a killed process are still found.
func signalGrow(procs []signalProc, members, groups map[int]bool) {
	self := syscall.Getpgrp()
	for grown := true; grown; {
		grown = false
		for _, p := range procs {
			if members[p.pid] || p.pgid == self || p.pid == os.Getpid() {
				continue
			}
			if members[p.ppid] || groups[p.pgid] {
				members[p.pid], groups[p.pgid], grown = true, true, true
			}
		}
	}
}

// signalReap kills the process rooted at pid, everything below it, and every
// group those use (a provider runs in a group of its own), until none is left.
// It reports whether the host was observed free of them before the deadline;
// a failed observation reports false.
func signalReap(root int) bool {
	members, groups := map[int]bool{root: true}, map[int]bool{root: true}
	deadline := time.Now().Add(5 * time.Second)
	for {
		procs, err := signalProcesses()
		if err != nil {
			return false
		}
		signalGrow(procs, members, groups)
		var live []int
		for _, p := range procs {
			if members[p.pid] {
				live = append(live, p.pid)
			}
		}
		if len(live) == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		for g := range groups {
			_ = syscall.Kill(-g, syscall.SIGKILL)
		}
		for _, pid := range live {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// signalDescendants returns the live processes below root or in its group.
func signalDescendants(root int) []signalProc {
	members, groups := map[int]bool{root: true}, map[int]bool{root: true}
	procs, err := signalProcesses()
	if err != nil {
		return nil
	}
	signalGrow(procs, members, groups)
	var below []signalProc
	for _, p := range procs {
		if members[p.pid] && p.pid != root {
			below = append(below, p)
		}
	}
	return below
}

// signalPidLive reports whether pid is live; when the host cannot be observed
// it stays true, since absence is not proven.
func signalPidLive(pid int) bool {
	procs, err := signalProcesses()
	if err != nil {
		return true
	}
	for _, p := range procs {
		if p.pid == pid {
			return true
		}
	}
	return false
}
