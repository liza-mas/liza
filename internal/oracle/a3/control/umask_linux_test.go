package control

import "syscall"

func setUmask() { syscall.Umask(0o022) }
