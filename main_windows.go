package main

import (
	"errors"
	"time"
)

func makeFIFO(targetBase string, mode uint32, mtime time.Time) error {
	return errors.Errorf("%s Windows does not support FIFOs in the file system", targetBase)
}

func makeSocket(targetBase string, mode uint32, mtime time.Time) error {
	return errors.Errorf("%s decode socket not yet implemented on Windows", targetBase)
}
