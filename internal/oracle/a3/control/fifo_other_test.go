//go:build !linux

package control

import "errors"

func makeFifo(string) error { return errors.New("fifo unsupported") }
