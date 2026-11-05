//go:build windows

package subprocess

import (
	"os/exec"
	"runtime"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Start starts cmd. When cmd was given ConfigureCancellation, its process is
// placed in a Job Object, so that cancellation terminates every descendant at
// once: the descendants are found by membership, not by walking parent PIDs
// with taskkill, which is bounded to one second and misses the descendants of
// an already exited child.
//
// Ceiling: a descendant spawned before the assignment just after process
// creation escapes the job; taskkill, still the fallback, may catch it. Closing
// that window would need CREATE_SUSPENDED and a resume of the child's thread,
// which os/exec does not expose.
func Start(cmd *exec.Cmd) error {
	if cmd.Cancel == nil {
		return cmd.Start()
	}
	job, err := newProcessJob()
	if err != nil {
		return cmd.Start() // taskkill remains the cancellation
	}
	fallback := cmd.Cancel
	cmd.Cancel = func() error {
		if job.terminate() {
			return nil
		}
		return fallback()
	}
	if err := cmd.Start(); err != nil {
		_ = windows.CloseHandle(job.handle)
		return err
	}
	// cmd.Cancel may run until Wait returns, which callers may defer: close the
	// handle once cmd itself is unreachable. Closing it does not kill members.
	runtime.AddCleanup(cmd, func(h windows.Handle) { _ = windows.CloseHandle(h) }, job.handle)
	_ = job.assign(cmd.Process.Pid) // on failure, cancellation falls back to taskkill
	return nil
}

// processJob is a Job Object holding one command's process tree.
type processJob struct {
	handle   windows.Handle
	assigned atomic.Bool
}

func newProcessJob() (*processJob, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	// A descendant that asks to break away (CREATE_BREAKAWAY_FROM_JOB) must
	// still start, as it would outside the job.
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK
	if _, err := windows.SetInformationJobObject(handle, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	return &processJob{handle: handle}, nil
}

// assign adds the started process to the job; its later descendants join the
// job with it.
func (j *processJob) assign(pid int) error {
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(process) }()
	if err := windows.AssignProcessToJobObject(j.handle, process); err != nil {
		return err
	}
	j.assigned.Store(true)
	return nil
}

// terminate kills every process in the job and reports whether it did.
func (j *processJob) terminate() bool {
	return j.assigned.Load() && windows.TerminateJobObject(j.handle, 1) == nil
}
