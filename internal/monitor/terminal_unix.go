//go:build darwin || linux

package monitor

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

func terminalWidth(file *os.File) int {
	var size struct{ rows, columns, xPixels, yPixels uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&size)))
	runtime.KeepAlive(file)
	if errno != 0 {
		return 0
	}
	return int(size.columns)
}
