package cli

import (
	"os"
	"syscall"
	"unsafe"
)

// isTerminal reports whether f is a terminal, so there's someone to answer
// questions. /dev/null is a character device too, so the file mode can't tell;
// only a terminal answers the termios ioctl
func isTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), ioctlGetTermios, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}
