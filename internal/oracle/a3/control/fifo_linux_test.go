package control

import "syscall"

func makeFifo(path string) error { return syscall.Mkfifo(path, 0o600) }
