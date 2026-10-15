package monitor

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAnimationFinishesWithAccurateCounts(t *testing.T) {
	for _, s := range []Stats{
		{Total: 120, Limit: 400, Percent: 30, Added: 92, Deleted: 28, State: "OK", Animate: true},
		{Total: 600, Limit: 400, Percent: 150, Added: 500, Deleted: 100, State: "OVER LIMIT", Animate: true},
		{Total: 0, Limit: 400, State: "OK", Animate: true},
	} {
		var output bytes.Buffer
		var elapsed time.Duration
		if err := animateBar(&output, s, func(d time.Duration) { elapsed += d }); err != nil {
			t.Fatal(err)
		}
		frames := strings.Split(strings.TrimPrefix(output.String(), "\r"), "\r")
		if frames[len(frames)-1] != Render(s) {
			t.Fatalf("wrong final frame: %q", output.String())
		}
		if strings.Count(output.String(), "\n") != 1 {
			t.Fatal("animation emitted extra lines")
		}
		summary := strings.TrimSuffix(strings.SplitN(Render(s), "] ", 2)[1], "\n")
		lastFill := -1
		for _, frame := range frames {
			if fill := strings.Count(frame, "#"); fill < lastFill || fill > barWidth {
				t.Fatal("bar did not fill monotonically")
			} else {
				lastFill = fill
			}
			if !strings.Contains(frame, summary) {
				t.Fatal("counts changed while animating")
			}
		}
		if elapsed > 240*time.Millisecond {
			t.Fatalf("animation too slow: %v", elapsed)
		}
	}
}

func TestAnimationEligibility(t *testing.T) {
	s := Stats{Animate: true, Percent: 50, Total: 200, Limit: 400, State: "OK"}
	if !canAnimate(s, 120, "xterm-256color", "") {
		t.Fatal("interactive terminal should animate")
	}
	for _, tc := range []struct {
		width    int
		term, ci string
	}{{0, "xterm", ""}, {20, "xterm", ""}, {120, "dumb", ""}, {120, "", ""}, {120, "xterm", "true"}} {
		if canAnimate(s, tc.width, tc.term, tc.ci) {
			t.Fatalf("should be static: %+v", tc)
		}
	}
	s.Animate = false
	if canAnimate(s, 120, "xterm", "") {
		t.Fatal("animation should be opt-in")
	}
}

func TestRedirectedOutputIsStatic(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("CI", "")
	f, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := Stats{Animate: true, Total: 400, Limit: 400, Percent: 100, State: "LIMIT REACHED"}
	if err := Display(f, s); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != Render(s) {
		t.Fatalf("redirected output contains frames: %q", data)
	}
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	if terminalWidth(null) != 0 {
		t.Fatal("a character device is not necessarily a terminal")
	}
}

type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) { return 0, errors.New("closed output") }
func TestAnimationStopsOnWriteFailure(t *testing.T) {
	if err := animateBar(failingWriter{}, Stats{Percent: 50}, func(time.Duration) { t.Fatal("slept after write failure") }); err == nil {
		t.Fatal("ignored write failure")
	}
}
