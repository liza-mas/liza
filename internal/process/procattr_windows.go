//go:build windows

package process

import (
	"os/exec"
	"syscall"
)

// SetDetachedProcessGroup puts the child in its own console process group.
//
// Windows has no Unix process groups, but it does have console process groups,
// and a child started without this flag joins its parent's. A Ctrl+C or a
// console-close event delivered to the parent is then delivered to every agent
// it spawned — so closing the TUI would take the agents with it, which is the
// opposite of what spawning them detached is for.
//
// CREATE_NEW_PROCESS_GROUP is the narrow answer: the child stops receiving the
// parent's console events while keeping the console itself. DETACHED_PROCESS
// would go further and give it none, but the agent spawns a provider CLI of its
// own, and taking the console away from those is a change this does not need to
// make.
func SetDetachedProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
}
