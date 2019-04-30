// +build darwin dragonfly freebsd netbsd openbsd linux

package main

import (
	"net"
	"os"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

func makeFIFO(targetBase string, mode uint32, mtime time.Time) error {
	err := unix.Mkfifo(targetBase, mode)
	if err != nil {
		return errors.Wrap(err, "cannot mkfifo")
	}
	return errors.Wrap(os.Chtimes(targetBase, mtime, mtime), "cannot chtimes")
}

func makeSocket(targetBase string, mode uint32, mtime time.Time) error {
	return errors.Errorf("%s decode socket not implemented", targetBase)

	if false {
		if err := unix.Mknod(string(targetBase), uint32(mode), 0); err != nil {
			return errors.Wrap(err, "cannot decode named pipe")
		}
	} else {
		l, err := net.Listen("unix", string(targetBase))
		if err != nil {
			return errors.Wrap(err, "cannot decode named pipe")
		}
		_ = l
		if false { // it turns out that closing the listener unlinks the socket
			if err = l.Close(); err != nil {
				return errors.Wrap(err, "cannot decode named pipe")
			}
		}
	}
	return errors.Wrap(os.Chtimes(string(targetBase), mtime, mtime), "cannot decode named pipe")
}

// FIXME: unix only (add windows stub)
func chmod(dirfd int, path string, mode uint32, flags int) error {
	return unix.Fchmodat(dirfd, path, mode, flags)
}
