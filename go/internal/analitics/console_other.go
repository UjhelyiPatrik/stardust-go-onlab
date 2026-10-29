//go:build !windows

package analytics

func enableVirtualTerminal() {
	// ANSI escape codes and UTF-8 are natively supported on Unix terminals.
}
