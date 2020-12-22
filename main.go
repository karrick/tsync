package main // import "github.com/karrick/tsync"

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/OneOfOne/xxhash"
	"github.com/karrick/gobsp"
	"github.com/karrick/godirwalk"
	"github.com/karrick/golf"
	"github.com/pkg/errors"
)

var dirReadScratch = make([]byte, 64*1024)
var fileScratch *bytes.Buffer
var messageScratch *bytes.Buffer

func init() {
	fileScratch = bytes.NewBuffer(make([]byte, 1*1024*1024))
	messageScratch = bytes.NewBuffer(make([]byte, 1*1024*1024))
}

const (
	v1Syn              gobsp.MessageType = iota // 0 client opens connection with protocol version request
	v1SynAck                                    // 1 server responds with protocol version selection
	v1RegularFile                               // 2
	v1DirectoryDescend                          // 3
	v1DirectoryAscend                           // 4
	v1Symlink                                   // 5
	v1FIFO                                      // 6
	v1Socket                                    // 7
	v1Device                                    // 8
)

var (
	optChdir   = golf.String("chdir", "", "when extracting, change to this directory prior to extraction")
	optDebug   = golf.Bool("debug", false, "prints debugging when true")
	optFile    = golf.String("file", "-", "name of input or output file; - means stdin or stdout")
	optVerbose = golf.Bool("verbose", false, "prints verbose information to stderr")
)

func main() {
	golf.Parse()

	args := golf.Args()
	if len(args) == 0 {
		usage("expected sub-command")
	}

	cmd, args := args[0], args[1:]

	if *optChdir != "" {
		// Convert arguments to absolute so we can find them after changing
		// directories.
		var err error
		for i := 0; i < len(args); i++ {
			args[i], err = filepath.Abs(args[i])
			fatalWhenErr(err)
		}
		fatalWhenErr(os.Chdir(*optChdir))
	}

	switch cmd {
	case "create":
		fatalWhenErr(create(args))
	case "extract":
		fatalWhenErr(extract(args))
	default:
		usage(fmt.Sprintf("invalid sub-command: %q", cmd))
	}
}

func usage(message string) {
	exec := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "%s\n", message)
	fmt.Fprintf(os.Stderr, "usage: %s [--debug | --verbose] [--file -] create arg1 arg2...\n", exec)
	fmt.Fprintf(os.Stderr, "usage: %s [--chdir PATH] [--debug | --verbose] [--file -] [--chdir PATH] extract\n", exec)
	os.Exit(2)
}

// fatalWhenErr displays the error message and exits if err is not nil. When err is
// nil, it returns.
func fatalWhenErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] %s\n", err)
		os.Exit(1)
	}
}

func debug(format string, a ...interface{}) {
	if *optDebug {
		_, _ = fmt.Fprintf(os.Stderr, "[DEBUG] "+format, a...)
	}
}

func warning(format string, a ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, "[WARNING] "+format, a...)
}

func create(args []string) error {
	var err error
	var fh *os.File
	var composer *gobsp.Composer

	if *optFile == "-" {
		composer = gobsp.NewComposer(os.Stdout)
	} else {
		fh, err = os.Create(*optFile)
		if err != nil {
			return err
		}
		composer = gobsp.NewComposer(fh)
	}

	for _, arg := range args {
		if err := encodeTarget(composer, arg); err != nil {
			warning("%s: cannot encode: %+v\n", arg, err)
		}
	}

	// Flush the composer's buffer, and close file handle when open.
	err = composer.Close()
	if fh != nil {
		if err2 := fh.Close(); err == nil {
			err = err2
		}
	}
	return err
}

func extract(args []string) error {
	var err error
	var fh *os.File
	var r io.Reader

	if *optFile == "-" {
		r = os.Stdin
	} else {
		fh, err = os.Open(*optFile)
		if err != nil {
			return err
		}
		r = fh
	}

	scanner, err := gobsp.NewScanner(r, gobsp.Handlers(map[uint32]gobsp.MessageHandler{
		uint32(v1RegularFile):      decodeFile,
		uint32(v1DirectoryAscend):  decodeDirectoryAscend,
		uint32(v1DirectoryDescend): decodeDirectoryDescend,
		uint32(v1Symlink):          decodeSymlink,
		uint32(v1FIFO):             decodeFIFO,
		uint32(v1Socket):           decodeSocket,
		uint32(v1Device):           decodeDevice,
	}))
	if err != nil {
		if fh != nil {
			_ = fh.Close() // ignore secondary error
		}
		return err
	}

	for scanner.Scan() {
		if err = scanner.Handle(); err != nil {
			warning("%s\n", err)
		}
	}

	if err2 := scanner.Err(); err == nil {
		err = err2
	}

	if fh != nil {
		if err2 := fh.Close(); err == nil {
			err = err2
		}
	}

	return err
}

func encodeTarget(composer *gobsp.Composer, target string) error {
	var err error
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}
	de, err := godirwalk.NewDirent(target)
	if err != nil {
		return errors.Wrap(err, "cannot encode")
	}
	return encodeDirent(composer, filepath.Dir(target), de)
}

func encodeDirent(composer *gobsp.Composer, targetParent string, de *godirwalk.Dirent) error {
	if de.IsRegular() {
		return errors.Wrap(encodeFile(composer, targetParent, de.Name()), "cannot encode file")
	} else if de.IsDir() {
		return errors.Wrap(encodeDirectory(composer, targetParent, de.Name()), "cannot encode directory")
	} else if de.IsSymlink() {
		return errors.Wrap(encodeSymlink(composer, targetParent, de.Name()), "cannot encode symlink")
	} else if de.ModeType()&os.ModeNamedPipe != 0 {
		return errors.Wrap(encodeFIFO(composer, targetParent, de.Name()), "cannot encode FIFO")
	} else if de.ModeType()&os.ModeSocket != 0 {
		return errors.Wrap(encodeSocket(composer, targetParent, de.Name()), "cannot encode socket")
	} else if de.ModeType()&os.ModeDevice != 0 {
		// TODO: add support
	}
	return errors.Errorf("cannot encode item: file mode type not supported: %s", de.ModeType())
}

// encodeDirectory encodes directory to composer, including all the children of
// the directory. If something prevents encoding directory, then return
// early. When one of the children of the directory fails to encode, emit a
// warning message, but continue on to the other children.
func encodeDirectory(composer *gobsp.Composer, targetParent, targetBase string) error {
	targetFull := filepath.Join(targetParent, targetBase)
	debug("%s encode directory\n", targetFull)

	fi, err := os.Stat(targetFull)
	if err != nil {
		return errors.WithStack(err)
	}

	deChildren, err := godirwalk.ReadDirents(targetFull, dirReadScratch)
	if err != nil {
		return errors.WithStack(err)
	}

	if err = encodeDirectoryDescend(composer, fi); err != nil {
		return errors.WithStack(err)
	}

	// Only sort the children when want debugging because it takes time but is
	// not necessary.
	if *optDebug {
		sort.Sort(deChildren) // DEBUG for sorted order
	}

	for _, deChild := range deChildren {
		if err = encodeDirent(composer, targetFull, deChild); err != nil {
			warning("%s: %+s\n", filepath.Join(targetFull, deChild.Name()), err)
		}
	}

	// When leaving a directory, send its modification time to remote.  There is
	// no error recovery for this not working, because local and remote will be
	// in different directories.
	fatalWhenErr(encodeDirectoryAscend(composer, fi))

	return nil
}

func encodeDirectoryAscend(composer *gobsp.Composer, fi os.FileInfo) error {
	messageScratch.Reset()
	if err := gobsp.Int64(fi.ModTime().Unix()).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode modification time")
	}
	return composer.Compose(v1DirectoryAscend, messageScratch.Bytes())
}

func encodeDirectoryDescend(composer *gobsp.Composer, fi os.FileInfo) error {
	messageScratch.Reset()
	if err := gobsp.String(fi.Name()).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrapf(err, "cannot encode name")
	}
	if err := gobsp.Uint32(fi.Mode()).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrapf(err, "cannot encode mode")
	}
	return composer.Compose(v1DirectoryDescend, messageScratch.Bytes())
}

func encodeFile(composer *gobsp.Composer, targetParent, targetBase string) error {
	targetFull := filepath.Join(targetParent, targetBase)
	debug("%s encode file\n", targetFull)

	fh, err := os.Open(targetFull)
	if err != nil {
		return errors.WithStack(err)
	}

	fi, err := fh.Stat()
	if err != nil {
		_ = fh.Close() // ignore secondary error
		return errors.WithStack(err)
	}

	size := fi.Size()
	fileScratch.Reset()
	fileScratch.Grow(int(size))

	c, err := fileScratch.ReadFrom(fh)
	if err2 := fh.Close(); err == nil {
		err = err2
	}
	if err != nil {
		return errors.WithStack(err)
	}
	if c < size {
		return errors.Wrapf(io.ErrUnexpectedEOF, "read fewer than expected bytes: %d < %d", c, size)
	}

	h := xxhash.New64()
	_, err = h.Write(fileScratch.Bytes())
	if err != nil {
		// While hash ought never return error, should protect against a
		// misbehaving hash if someday which library is changed.
		return errors.Wrap(err, "cannot calculate hash")
	}

	messageScratch.Reset()

	if err = gobsp.String(targetBase).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode name")
	}

	debug("%s mtime: %v\n", targetBase, fi.ModTime().UTC().Unix())
	if err = gobsp.Int64(fi.ModTime().UTC().Unix()).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode modification time")
	}

	debug("%s mode: %v\n", targetBase, fi.Mode())
	if err = gobsp.Uint32(uint32(fi.Mode())).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode mode")
	}

	debug("%s hash: % x\n", targetBase, h.Sum64())
	if err = gobsp.Uint64(h.Sum64()).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode hash")
	}

	if err = gobsp.UVWI(size).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode size")
	}

	messageScratch.Grow(int(size))
	if _, err = fileScratch.WriteTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode contents")
	}

	return composer.Compose(v1RegularFile, messageScratch.Bytes())
}

func encodeSymlink(composer *gobsp.Composer, targetParent, targetBase string) error {
	targetFull := filepath.Join(targetParent, targetBase)
	debug("%s encode symlink\n", targetFull)
	debug("%s targetParent: %s\n", targetBase, targetParent)

	li, err := os.Lstat(string(targetFull))
	if err != nil {
		return errors.WithStack(err)
	}

	linkname, err := os.Readlink(targetFull)
	if err != nil {
		return errors.WithStack(err)
	}

	messageScratch.Reset()

	// name
	if err = gobsp.String(targetBase).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode name")
	}

	debug("%s referent: %v\n", targetBase, linkname)
	if err = gobsp.String(linkname).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode referent")
	}

	mtime := li.ModTime().UTC().Unix()
	debug("%s mtime: %v\n", targetBase, mtime)
	if err = gobsp.Int64(mtime).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode modification time")
	}

	mode := li.Mode()
	debug("%s mode: %v\n", targetBase, mode)
	if err = gobsp.Uint32(uint32(li.Mode())).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode mode")
	}

	return composer.Compose(v1Symlink, messageScratch.Bytes())
}

func encodeFIFO(composer *gobsp.Composer, targetParent, targetBase string) error {
	targetFull := filepath.Join(targetParent, targetBase)
	debug("%s encode fifo\n", targetFull)

	fi, err := os.Stat(targetFull)
	if err != nil {
		return errors.WithStack(err)
	}

	messageScratch.Reset()

	if err = gobsp.String(targetBase).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode name")
	}

	debug("%s mtime: %v\n", targetBase, fi.ModTime().UTC().Unix())
	if err = gobsp.Int64(fi.ModTime().UTC().Unix()).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode modification time")
	}

	debug("%s mode: %v\n", targetBase, fi.Mode())
	if err = gobsp.Uint32(uint32(fi.Mode())).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode mode")
	}

	return composer.Compose(v1FIFO, messageScratch.Bytes())
}

func encodeSocket(composer *gobsp.Composer, targetParent, targetBase string) error {
	targetFull := filepath.Join(targetParent, targetBase)
	debug("%s encode socket\n", targetFull)

	fi, err := os.Stat(targetFull)
	if err != nil {
		return errors.Wrap(err, "cannot encode socket")
	}

	messageScratch.Reset()

	// name
	if err = gobsp.String(targetBase).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode socket name")
	}
	// mtime
	debug("%s mtime: %v\n", targetBase, fi.ModTime().UTC().Unix())
	if err = gobsp.Int64(fi.ModTime().UTC().Unix()).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode socket modification time")
	}
	// mode
	debug("%s mode: %v\n", targetBase, fi.Mode())
	if err = gobsp.Uint32(uint32(fi.Mode())).MarshalBinaryTo(messageScratch); err != nil {
		return errors.Wrap(err, "cannot encode socket mode")
	}

	return errors.Wrap(composer.Compose(v1Socket, messageScratch.Bytes()), "cannot encode socket")
}

func decodeDevice(r io.Reader) error {
	var err error
	var targetBase gobsp.String

	if err = targetBase.UnmarshalBinaryFrom(r); err != nil {
		return err
	}
	debug("%s decode device\n", targetBase)
	return errors.Errorf("%s decode device not implemented", targetBase)
}

func decodeDirectoryAscend(r io.Reader) error {
	var mtime gobsp.Int64
	if err := mtime.UnmarshalBinaryFrom(r); err != nil {
		return err
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	t := time.Unix(int64(mtime), 0)
	if err = os.Chtimes(wd, t, t); err != nil {
		return err
	}

	return os.Chdir(filepath.Dir(wd))
}

func decodeDirectoryDescend(r io.Reader) error {
	var targetBase gobsp.String
	fatalWhenErr(targetBase.UnmarshalBinaryFrom(r))
	debug("%s decode directory\n", targetBase)

	var mode gobsp.Uint32
	fatalWhenErr(mode.UnmarshalBinaryFrom(r))

	// Use Lstat to check whether file system object currently with same name
	// exists and is not a directory.
	fi, err := os.Lstat(string(targetBase))
	if err != nil {
		if !os.IsNotExist(err) {
			fatalWhenErr(err) // unknown error
		}
		// targetBase does not exist; create a directory and descend. Use less
		// restrictive os.ModePerm for initial permissions, and will tighten
		// down when we leave this directory.
		fatalWhenErr(os.Mkdir(string(targetBase), os.FileMode(mode)))
		fatalWhenErr(os.Chdir(string(targetBase)))
	} else {
		// targetBase exists
		if fi.IsDir() {
			// targetBase already directory, so ensure permissions allow then descend
			fatalWhenErr(os.Chdir(string(targetBase)))
		} else {
			// targetBase not directory, but should be
			fatalWhenErr(os.Remove(string(targetBase)))
			fatalWhenErr(os.Mkdir(string(targetBase), os.FileMode(mode)))
			fatalWhenErr(os.Chdir(string(targetBase)))
		}
	}
	return nil
}

func decodeFIFO(r io.Reader) error {
	var err error
	var targetBase gobsp.String
	var mtime gobsp.Int64
	var mode gobsp.Uint32

	if err = targetBase.UnmarshalBinaryFrom(r); err != nil {
		return err
	}
	debug("%s decode fifo\n", targetBase)

	if err = mtime.UnmarshalBinaryFrom(r); err != nil {
		return err
	}

	if err = mode.UnmarshalBinaryFrom(r); err != nil {
		return err
	}

	// When exists, but wrong type...
	fi, err := os.Lstat(string(targetBase))
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.WithStack(err)
		}
	} else if fi.Mode()&os.ModeNamedPipe == 0 {
		if err = os.RemoveAll(string(targetBase)); err != nil {
			return errors.WithStack(err)
		}
	}

	return makeFIFO(string(targetBase), uint32(mode), time.Unix(int64(mtime), 0))
}

func decodeFile(r io.Reader) error {
	var err error
	var targetBase gobsp.String
	var mtime gobsp.Int64
	var mode gobsp.Uint32
	var hashSource gobsp.Uint64
	var size gobsp.UVWI

	if err = targetBase.UnmarshalBinaryFrom(r); err != nil {
		return errors.Wrap(err, "cannot decode name")
	}
	debug("%s decode file\n", targetBase)

	if err = mtime.UnmarshalBinaryFrom(r); err != nil {
		return errors.Wrap(err, "cannot decode modification time")
	}

	if err = mode.UnmarshalBinaryFrom(r); err != nil {
		return errors.Wrap(err, "cannot decode mode")
	}

	if err = hashSource.UnmarshalBinaryFrom(r); err != nil {
		return errors.Wrap(err, "cannot decode hash")
	}

	if err = size.UnmarshalBinaryFrom(r); err != nil {
		return errors.Wrap(err, "cannot decode size")
	}

	//
	// ??? Consider adding optimizations to elide overwrite based on mtime,
	// hash, mode, and size
	//

	// Read in file contents and validate hash
	hashDest := xxhash.New64()
	fileScratch.Reset()
	fileScratch.Grow(int(size))
	c, err := fileScratch.ReadFrom(io.TeeReader(r, hashDest))
	if err != nil {
		return errors.WithStack(err)
	}
	if c < int64(size) {
		return errors.Wrapf(io.ErrUnexpectedEOF, "read fewer than expected bytes: %d < %d", c, size)
	}
	if hs, hd := uint64(hashSource), hashDest.Sum64(); hs != hd {
		return errors.Errorf("hash mismatch: % x != % x", hs, hd)
	}

	// When exists, but wrong type...
	fi, err := os.Lstat(string(targetBase))
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.WithStack(err)
		}
	} else if !fi.Mode().IsRegular() {
		if err = os.RemoveAll(string(targetBase)); err != nil {
			return errors.WithStack(err)
		}
	}

	//
	// TODO: deal with situation when requested permissions prevent
	// modifications
	//

	fh, err := os.OpenFile(string(targetBase), os.O_RDWR|os.O_CREATE, os.ModePerm)
	if err != nil {
		return errors.WithStack(err)
	}

	// Read from tee reader, causing data to be also written to hash.
	c, err = fileScratch.WriteTo(fh)
	if err != nil {
		_ = fh.Close() // ignore secondary error
		return errors.WithStack(err)
	}
	if c < int64(size) {
		_ = fh.Close() // ignore secondary error
		return errors.Wrapf(io.ErrShortWrite, "%d < %d", c, size)
	}

	// Truncate file after size bytes to handle smaller source than destination.
	if err = fh.Truncate(c); err != nil {
		_ = fh.Close() // ignore secondary error
		return errors.WithStack(err)
	}
	if err = fh.Chmod(os.FileMode(mode).Perm()); err != nil {
		return errors.WithStack(err)
	}
	if err = fh.Close(); err != nil {
		return errors.WithStack(err)
	}
	t := time.Unix(int64(mtime), 0)
	return os.Chtimes(string(targetBase), t, t)
}

func decodeSocket(r io.Reader) error {
	var err error
	var targetBase gobsp.String
	var mtime gobsp.Int64
	var mode gobsp.Uint32

	if err = targetBase.UnmarshalBinaryFrom(r); err != nil {
		return err
	}
	debug("%s decode socket\n", targetBase)

	if err = mtime.UnmarshalBinaryFrom(r); err != nil {
		return err
	}

	if err = mode.UnmarshalBinaryFrom(r); err != nil {
		return err
	}

	// When exists, but wrong type...
	fi, err := os.Lstat(string(targetBase))
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.Wrap(err, "cannot decode named pipe")
		}
	} else if fi.Mode()&os.ModeSocket == 0 {
		if err = os.RemoveAll(string(targetBase)); err != nil {
			return errors.Wrap(err, "cannot decode named pipe")
		}
	}

	err = makeSocket(string(targetBase), uint32(mode), time.Unix(int64(mtime), 0))
	return errors.Wrap(err, "cannot decode socket")
}

func decodeSymlink(r io.Reader) error {
	var err error
	var targetBase gobsp.String // base name of symbolic link we are making
	var linkname gobsp.String   // symbolic link will point to this file system entry
	var mtime gobsp.Int64
	var mode gobsp.Uint32

	if err = targetBase.UnmarshalBinaryFrom(r); err != nil {
		return errors.Wrap(err, "cannot decode name")
	}
	debug("%s symlink\n", targetBase)
	if err = linkname.UnmarshalBinaryFrom(r); err != nil {
		return errors.Wrap(err, "cannot decode referent")
	}
	if err = mtime.UnmarshalBinaryFrom(r); err != nil {
		return errors.Wrap(err, "cannot decode modification time")
	}
	if err = mode.UnmarshalBinaryFrom(r); err != nil {
		return errors.Wrap(err, "cannot decode mode")
	}

	// When exists, but wrong type...
	fi, err := os.Lstat(string(targetBase))
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.WithStack(err)
		}
	} else if fi.Mode()&os.ModeSymlink == 0 {
		if err = os.RemoveAll(string(targetBase)); err != nil {
			return errors.WithStack(err)
		}
	}

	if err = os.Symlink(string(linkname), string(targetBase)); err != nil {
		return errors.WithStack(err)
	}

	// t := time.Unix(int64(mtime), 0)
	// if err = os.Chtimes(string(targetBase), t, t); err != nil {
	// 	return errors.WithStack(err)
	// }

	// if err = os.Chmod(string(targetBase), os.FileMode(mode).Perm()); err != nil {
	// 	return errors.WithStack(err)
	// }

	return nil

	// wd, err := os.Getwd()
	// if err != nil {
	// 	return errors.WithStack(err)
	// }

	// fh, err := os.Open(wd)
	// if err != nil {
	// 	return errors.WithStack(err)
	// }
	// defer fh.Close()

	// dirfd := int(fh.Fd())

	// targetFull := filepath.Join(wd, string(targetBase))
	// mode := uint32(fi.Mode()) // ??? why trying to get mode of possibly non existing entry
	// flags := unix.AT_SYMLINK_NOFOLLOW

	// return errors.WithStack(chmod(dirfd, targetFull, mode, flags))
}
