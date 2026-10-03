//go:build !linux

package ipc

import "net"

// samePeer has no portable peer-credential API outside Linux; the host token
// check plus the 0600 socket inside a 0700 directory carry the identity
// guarantee there.
func samePeer(net.Conn) bool { return true }
