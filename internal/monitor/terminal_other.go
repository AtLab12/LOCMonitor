//go:build !darwin && !linux

package monitor

import "os"

// Unsupported terminal APIs use static output.
func terminalWidth(file *os.File) int { return 0 }
