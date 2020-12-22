# tsync

`tsync` optimized transfer of file system entries over TCP socket.

## Status

EXPERIMENTAL

## Description

tsync is a very fast way to copy one or more file system entries from
source to destination over a network socket. While `rsync` is the
preferred choice for this particular task when synchronizing files,
when copying files for first time, `tsync` is much faster.

### Performance and Reliability

`rsync` is an amazing and fast program. I recommend anyone curious to
study its source code and learn how it works. However, for making a
bulk filesystem copy across a network, some of the features of `rsync`
make it more slow than merely streaming the file system object data
and metadata across the network.

I thouroughly tested `tsync` multiple times with nearly 900 GiB of
data, comparing the transfer time and the resultant file system
output. To test the speed, I simply invoked each command using the
`time` shell builtin command to measure the wall clock time to
transfer the ~900 GiB file system hierarchy.

On the low-end embedded devices I tested on, `tsync` repeatedly
performed the transfer in ~12 minutes, and `rsync` repeatedly
performed the same transfer to a newly initialized directory in ~45
minutes.

To test the correctness of `tsync`, after I ran it to transfer the
~900 GiB file system hierarchy, I ran `rsync` in verbose mode to
display any changes needed to make the destination a duplicate of the
source. In every test, the only differences were that rsync supports
unix domain sockets when tsync does not yet.

## Usage

### Simple Creation and Extraction of Archive Files

Create archive file consisting of one or more file system entries.

    $ tsync create --file ~/path/stuff.saf ~/foo ~/bar

Extract contents of archive file into 

    $ tsync extract --chdir ~/dest --file ~/path/stuff.saf

### Replication to another host

Always start `tsync` on the destination machine first. The receive
subcommand expects an optional IP address and a mandatory port number
to bind to. The port number must always be proceeded by the colon
character. The optional IP address would be used when `tsync` ought to
bind only to a particular interface, if desired.

    [you@destination.example.com ~]$ tcp-pipe receive :6969 | tsync extract --chdir ~/dest

After the recipient is waiting, send the files from the source.

    [you@source.example.com ~]$ tsync create ~/dir1 ~/dir2 ... | tcp-pipe send destination.example.com:6969

After `tsync` finishes, `~/dir1` and `~/dir2` from
`source.example.com` will be replicated to `~/dir1` and `~/dir2` on
`destination.example.com`.

### Verbose Output

By default `tsync` does not display any output on the source or
destination hosts unless any errors are encountered, in which case
errors are printed to standard error. When `tsync` is invoked with
the `-v, --verbose` command line flag, it prints connection
information and compression information to standard output. The
verbose flag on the source and destination are independent of each
other. In other words you may have verbose on neither of the source or
the destination, either of them, or both of them.

    [you@destination.example.com ~]$ tsync -v create --file foo.af ~/foo

## Limitations

Because each hard link to a file is identical to each other hard link
to the same file, the algorithm to determine whether a particular file
system entry is a unique file or merely a hard link to another file in
the same hierarchy is an O(n^2) problem that is not implemented
here. Furthermore, even if two unique files have the same data does
not mean the user wants a hard link created on the destination. As a
result hard links on the source are replicated on the destination as
merely another file that happens to have identical contents.

The following file system objects are not supported:

1. Extracting a FIFO (named pipe) is not supported on Windows™ (OS
   limitation), but is supported on UNIX™ like systems.
1. Block and Character devices (TODO)
1. UNIX™ domain sockets (TODO)
