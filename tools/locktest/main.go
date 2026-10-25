// locktest measures how a byte-range lock on an SMB share behaves when the
// network drops: the lock of restoresafe.lock (LockFileEx on byte 0). How
// to build and run it, and what it measured: README.md.
//
//	locktest hold  [-shared] <file>   take the lock, write a holder line, keep it
//	locktest probe <file>             try an exclusive lock every 5 s, report
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"golang.org/x/sys/windows"
)

const holderOffset = 512

func lock(f *os.File, exclusive bool) error {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	return windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, new(windows.Overlapped))
}

func unlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}

func stamp() string { return time.Now().Format("15:04:05") }

func hold(path string, shared bool) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		fmt.Println(stamp(), "open failed:", err)
		os.Exit(1)
	}
	if err := lock(f, !shared); err != nil {
		fmt.Println(stamp(), "lock failed:", err)
		os.Exit(1)
	}
	host, _ := os.Hostname()
	line := fmt.Sprintf("%s pid %d since %s\n", host, os.Getpid(), time.Now().Format(time.RFC3339))
	if !shared {
		if _, err := f.WriteAt([]byte(line), holderOffset); err != nil {
			fmt.Println(stamp(), "holder line not written:", err)
		}
		f.Sync()
	}
	kind := map[bool]string{false: "exclusive", true: "shared"}[shared]
	fmt.Printf("%s holding the %s lock (pid %d). Leave this window open.\n", stamp(), kind, os.Getpid())
	fmt.Println("Every 5 s it checks that its handle still works (a 1-byte read beyond the lock).")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	go func() {
		<-stop
		fmt.Println(stamp(), "Ctrl+C: closing the handle")
		err := f.Close()
		fmt.Println(stamp(), "handle closed, error:", err)
		os.Exit(0)
	}()
	buf := make([]byte, 1)
	lastOK := true
	for {
		time.Sleep(5 * time.Second)
		_, err := f.ReadAt(buf, holderOffset)
		// EOF comes from the server: the shared lock writes no holder line,
		// so the file is empty and the read is answered with end of file.
		ok := err == nil || errors.Is(err, io.EOF)
		if ok != lastOK {
			if ok {
				fmt.Println(stamp(), "handle works again")
			} else {
				fmt.Println(stamp(), "handle fails:", err)
			}
			lastOK = ok
		}
	}
}

func probe(path string) {
	start := time.Now()
	fmt.Println(stamp(), "probing every 5 s; Ctrl+C to stop")
	last := ""
	for {
		state := ""
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			state = "open failed: " + err.Error()
		} else {
			if err := lock(f, true); err != nil {
				state = "locked: " + err.Error()
				buf := make([]byte, 120)
				if n, rerr := f.ReadAt(buf, holderOffset); n > 0 {
					state += fmt.Sprintf(" | holder line readable: %q", string(buf[:n]))
				} else if rerr != nil {
					state += " | holder line not readable: " + rerr.Error()
				}
			} else {
				state = "FREE: the lock could be taken"
				unlock(f)
			}
			f.Close()
		}
		if state != last {
			fmt.Printf("%s (+%3.0f s) %s\n", stamp(), time.Since(start).Seconds(), state)
			last = state
		}
		time.Sleep(5 * time.Second)
	}
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: locktest hold [-shared] <file> | locktest probe <file>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "hold":
		fs := flag.NewFlagSet("hold", flag.ExitOnError)
		shared := fs.Bool("shared", false, "take the shared lock (restore, verify)")
		fs.Parse(os.Args[2:])
		hold(fs.Arg(0), *shared)
	case "probe":
		probe(os.Args[2])
	}
}
