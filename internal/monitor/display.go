package monitor

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Display animates only when stdout is a sufficiently wide terminal. It never
// emits terminal control characters to pipes, JSON output, or CI logs.
func Display(out *os.File, s Stats) error {
	if canAnimate(s, terminalWidth(out), os.Getenv("TERM"), os.Getenv("CI")) {
		return animateBar(out, s, time.Sleep)
	}
	_, err := io.WriteString(out, Render(s))
	return err
}

func canAnimate(s Stats, width int, term, ci string) bool {
	// Reserve one column to avoid wrapping. All rendered text is ASCII.
	return s.Animate && barFill(s) > 0 && term != "" && term != "dumb" && ci == "" && width >= len(Render(s))
}

func animateBar(out io.Writer, s Stats, sleep func(time.Duration)) error {
	fill := barFill(s)
	if fill == 0 {
		_, err := io.WriteString(out, Render(s))
		return err
	}
	const duration = 240 * time.Millisecond
	for i := 0; i < fill; i++ {
		if _, err := fmt.Fprint(out, "\r", strings.TrimSuffix(renderFill(s, i), "\n")); err != nil {
			return err
		}
		sleep(duration / time.Duration(fill))
	}
	_, err := fmt.Fprint(out, "\r", Render(s))
	return err
}
