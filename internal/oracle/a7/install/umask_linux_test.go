//go:build linux

package install

import "syscall"

func setUmask() { syscall.Umask(0o022) }
