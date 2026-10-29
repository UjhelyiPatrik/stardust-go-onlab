//go:build windows

package analytics

import (
	"os"
	"syscall"
)

const enableVirtualTerminalProcessing uint32 = 0x0004

func enableVirtualTerminal() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleOutputCP := kernel32.NewProc("SetConsoleOutputCP")
	_, _, _ = setConsoleOutputCP.Call(65001) // UTF-8 code page

	handle := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	if err := syscall.GetConsoleMode(handle, &mode); err == nil {
		setConsoleMode := kernel32.NewProc("SetConsoleMode")
		_, _, _ = setConsoleMode.Call(uintptr(handle), uintptr(mode|enableVirtualTerminalProcessing))
	}
}
