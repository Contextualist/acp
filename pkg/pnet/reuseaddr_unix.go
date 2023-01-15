//go:build !windows && !wasm

package pnet

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func control(network, address string, c syscall.RawConn) error {
	var err error
	_ = c.Control(func(fd uintptr) {
		err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
		if err != nil {
			return
		}
		err = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		if err != nil {
			return
		}
	})
	return err
}
