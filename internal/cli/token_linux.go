//go:build linux

package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"unsafe"
)

func terminal(fd uintptr, request uintptr, value *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(unsafe.Pointer(value)))
	if errno != 0 {
		return errno
	}
	return nil
}

// Hidden import fails closed: only a tty whose echo can be disabled is accepted.
func hiddenToken(in io.Reader, stderr io.Writer) (raw []byte, err error) {
	file, ok := in.(*os.File)
	if !ok {
		return nil, errors.New("Secret import requires a terminal or explicit --token-stdin.")
	}
	var original syscall.Termios
	if terminal(file.Fd(), syscall.TCGETS, &original) != nil {
		return nil, errors.New("Secret import requires a terminal or explicit --token-stdin.")
	}
	// Catch termination before changing terminal state.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(interrupts)
	hidden := original
	// Canonical tty input silently truncates long lines; bound input ourselves instead.
	hidden.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON
	// Timed synchronous reads avoid a cancelled goroutine stealing subsequent input.
	hidden.Cc[syscall.VMIN] = 0
	hidden.Cc[syscall.VTIME] = 1
	if terminal(file.Fd(), syscall.TCSETS, &hidden) != nil {
		return nil, errors.New("Hidden token input is unavailable; use explicit --token-stdin securely.")
	}
	defer func() {
		if terminal(file.Fd(), syscall.TCSETS, &original) != nil {
			raw = nil
			err = errors.New("Hidden token input could not restore terminal state.")
		}
		fmt.Fprintln(stderr)
	}()
	if _, err = fmt.Fprint(stderr, "Claude OAuth token (hidden): "); err != nil {
		return nil, err
	}
	value := make([]byte, 0, 128)
	for len(value) < 16387 {
		select {
		case <-interrupts:
			return nil, errCancelled
		default:
		}
		var b [1]byte
		// syscall.Read keeps a tty timeout's zero-byte result distinct from EOF.
		n, readErr := syscall.Read(int(file.Fd()), b[:])
		if readErr == syscall.EINTR {
			continue
		}
		if readErr != nil {
			return nil, errors.New("Hidden token input is unavailable; use explicit --token-stdin securely.")
		}
		if n == 0 {
			var current syscall.Termios
			if terminal(file.Fd(), syscall.TCGETS, &current) != nil {
				return nil, errCancelled
			}
			continue
		}
		if b[0] == original.Cc[syscall.VEOF] {
			return nil, errCancelled
		}
		if b[0] == original.Cc[syscall.VERASE] || b[0] == 127 {
			if len(value) > 0 {
				value = value[:len(value)-1]
			}
			continue
		}
		if b[0] == original.Cc[syscall.VKILL] {
			value = value[:0]
			continue
		}
		value = append(value, b[0])
		if b[0] == '\n' {
			return value, nil
		}
	}
	return nil, errors.New("Token must be a nonempty single value without whitespace.")
}
