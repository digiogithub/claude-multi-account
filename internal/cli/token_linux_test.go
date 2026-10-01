//go:build linux

package cli

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

type promptWriter struct {
	bytes.Buffer
	ready chan struct{}
}

func (w *promptWriter) Write(p []byte) (int, error) {
	n, e := w.Buffer.Write(p)
	if strings.Contains(string(p), "(hidden)") {
		close(w.ready)
	}
	return n, e
}

func TestHiddenTokenDisablesEchoAndRestores(t *testing.T) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skip("No Linux PTY available")
	}
	defer master.Close()
	var unlock int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock)))
	if errno != 0 {
		t.Fatal(errno)
	}
	var number uint32
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&number)))
	if errno != 0 {
		t.Fatal(errno)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	var original syscall.Termios
	if err := terminal(slave.Fd(), syscall.TCGETS, &original); err != nil {
		t.Fatal(err)
	}
	writer := &promptWriter{ready: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		token, err := readToken(slave, writer, false)
		if err == nil && token != "synthetic-hidden" {
			err = fmt.Errorf("Wrong hidden token")
		}
		done <- err
	}()
	select {
	case <-writer.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("Hidden input did not prompt")
	}
	var hidden syscall.Termios
	if err := terminal(slave.Fd(), syscall.TCGETS, &hidden); err != nil {
		t.Fatal(err)
	}
	if hidden.Lflag&(syscall.ECHO|syscall.ECHONL) != 0 {
		t.Fatal("Echo still enabled during token input")
	}
	if _, err := master.Write([]byte("synthetic-hidden\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Hidden input did not finish")
	}
	var restored syscall.Termios
	if err := terminal(slave.Fd(), syscall.TCGETS, &restored); err != nil {
		t.Fatal(err)
	}
	if restored != original {
		t.Fatal("Terminal settings were not restored")
	}
	if strings.Contains(writer.String(), "synthetic-hidden") {
		t.Fatal("Hidden token printed")
	}
}

func TestHiddenTokenPipeFailsClosed(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	var stderr bytes.Buffer
	if _, err := readToken(reader, &stderr, false); err == nil || !strings.Contains(err.Error(), "explicit --token-stdin") {
		t.Fatal("Pipe accepted without explicit stdin flag")
	}
}

func TestHiddenTokenInterruptRestoresWithoutFurtherInput(t *testing.T) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skip("No Linux PTY available")
	}
	defer master.Close()
	var unlock int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock)))
	if errno != 0 {
		t.Fatal(errno)
	}
	var number uint32
	_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&number)))
	if errno != 0 {
		t.Fatal(errno)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	var original syscall.Termios
	if err := terminal(slave.Fd(), syscall.TCGETS, &original); err != nil {
		t.Fatal(err)
	}
	writer := &promptWriter{ready: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := readToken(slave, writer, false)
		done <- err
	}()
	select {
	case <-writer.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("Hidden input did not prompt")
	}
	// Let the input goroutine enter its blocking read before delivering SIGINT.
	time.Sleep(100 * time.Millisecond)
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != errCancelled {
			t.Fatalf("Expected cancellation, got %v", err)
		}
	case <-time.After(2 * time.Second):
		// Release the blocked reader before failing, so the test does not leak it.
		_, _ = master.Write([]byte("\n"))
		<-done
		t.Fatal("Cancellation waited for further terminal input")
	}
	var restored syscall.Termios
	if err := terminal(slave.Fd(), syscall.TCGETS, &restored); err != nil {
		t.Fatal(err)
	}
	if restored != original {
		t.Fatal("Interrupted input did not restore terminal settings")
	}
	// A cancelled background read must not steal the next operation's input.
	if _, err := master.Write([]byte("next\n")); err != nil {
		t.Fatal(err)
	}
	var next [32]byte
	n, err := slave.Read(next[:])
	if err != nil || string(next[:n]) != "next\n" {
		t.Fatalf("Cancelled reader consumed subsequent input: %q, %v", next[:n], err)
	}
}
