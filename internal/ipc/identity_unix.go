//go:build linux

package ipc

import (
	"net"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// samePeer verifies the connected client runs as the same user as the host.
// The socket itself lives in a 0700 directory with 0600 permissions, so this
// is defense in depth: even a process that can reach the socket path cannot
// be a different account.
func samePeer(conn net.Conn) bool {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return false
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return false
	}
	var cred *unix.Ucred
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		cred, sockErr = unix.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return false
	}
	if sockErr != nil {
		return false
	}
	return cred != nil && cred.Uid == uint32(os.Getuid())
}
