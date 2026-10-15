package monitor

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func Git(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func Root(dir string) (string, error) {
	out, err := Git(dir, "rev-parse", "--show-toplevel")
	return strings.TrimSpace(string(out)), err
}

type Stats struct {
	Animate   bool    `json:"-"`
	Base      string  `json:"base"`
	MergeBase string  `json:"merge_base"`
	Added     int     `json:"added"`
	Deleted   int     `json:"deleted"`
	Binary    int     `json:"binary_files"`
	Files     int     `json:"files"`
	Total     int     `json:"total"`
	Limit     int     `json:"limit"`
	Percent   float64 `json:"percent"`
	State     string  `json:"state"`
}

func ResolveBase(root, base string) (string, error) {
	if base != "auto" {
		return base, nil
	}
	if b, err := Git(root, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimSpace(string(b)), nil
	}
	for _, b := range []string{"refs/remotes/origin/main", "refs/heads/main", "refs/remotes/origin/master", "refs/heads/master"} {
		if _, err := Git(root, "rev-parse", "--verify", b+"^{commit}"); err == nil {
			return b, nil
		}
	}
	return "", fmt.Errorf("cannot find a base branch; set base in this repository’s global config entry (and fetch it if needed)")
}

func Measure(dir string) (Stats, error) {
	var s Stats
	root, err := Root(dir)
	if err != nil {
		return s, err
	}
	c, err := Load(root)
	if err != nil {
		return s, err
	}
	base, err := ResolveBase(root, c.Base)
	if err != nil {
		return s, err
	}
	baseID, err := Git(root, "rev-parse", "--verify", base+"^{commit}")
	if err != nil {
		return s, err
	}
	headID, err := Git(root, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return s, err
	}
	merge, err := Git(root, "merge-base", strings.TrimSpace(string(baseID)), strings.TrimSpace(string(headID)))
	if err != nil {
		return s, fmt.Errorf("cannot find common history with %s; fetch missing history if this is a shallow clone: %w", base, err)
	}
	s.Base = base
	s.MergeBase = strings.TrimSpace(string(merge))
	s.Limit = c.Limit
	s.Animate = c.Animate
	args := []string{"diff", "--numstat", "-z", "--no-ext-diff", "--no-textconv", "--find-renames", s.MergeBase, strings.TrimSpace(string(headID)), "--", "."}
	for _, p := range c.Exclude {
		args = append(args, ":(top,exclude,glob)"+p)
	}
	for _, extension := range c.ExcludeExtensions {
		args = append(args, ":(top,exclude,glob)**/*."+extension)
	}
	for _, folder := range c.ExcludeFolders {
		// Literal paths keep spaces and glob characters in directory names literal.
		// The trailing slash limits the match to the directory and its descendants.
		args = append(args, ":(top,exclude,literal)"+folder+"/")
	}
	out, err := Git(root, args...)
	if err != nil {
		return s, err
	}
	if err := parseNumstat(out, &s); err != nil {
		return s, err
	}
	s.Total = s.Added + s.Deleted
	s.Percent = float64(s.Total) * 100 / float64(s.Limit)
	s.State = "OK"
	if s.Percent >= float64(c.WarnAt) {
		s.State = "NEAR LIMIT"
	}
	if s.Total >= s.Limit {
		s.State = "LIMIT REACHED"
	}
	if s.Total > s.Limit {
		s.State = "OVER LIMIT"
	}
	return s, nil
}

func parseNumstat(data []byte, s *Stats) error {
	fields := bytes.Split(data, []byte{0})
	for i := 0; i < len(fields); i++ {
		if len(fields[i]) == 0 {
			continue
		}
		row := bytes.SplitN(fields[i], []byte{'\t'}, 3)
		if len(row) != 3 {
			return fmt.Errorf("invalid git numstat output")
		}
		if len(row[2]) == 0 { // With -z, renames have separate old and new path fields.
			if i+2 >= len(fields) {
				return fmt.Errorf("incomplete rename in git numstat output")
			}
			i += 2
		}
		s.Files++
		if string(row[0]) == "-" && string(row[1]) == "-" {
			s.Binary++
			continue
		}
		a, err := strconv.Atoi(string(row[0]))
		if err != nil {
			return err
		}
		d, err := strconv.Atoi(string(row[1]))
		if err != nil {
			return err
		}
		s.Added += a
		s.Deleted += d
	}
	return nil
}

const barWidth = 24

func barFill(s Stats) int {
	fill := int(s.Percent * barWidth / 100)
	if fill > barWidth {
		return barWidth
	}
	if fill < 0 {
		return 0
	}
	return fill
}

func Render(s Stats) string { return renderFill(s, barFill(s)) }

// Only the bar changes during animation; the counts always show the real diff.
func renderFill(s Stats, fill int) string {
	result := fmt.Sprintf("LOCMonitor [%s%s] %d/%d LOC (%.0f%%) | +%d -%d | %s", strings.Repeat("#", fill), strings.Repeat("-", barWidth-fill), s.Total, s.Limit, s.Percent, s.Added, s.Deleted, s.State)
	if s.Binary > 0 {
		result += fmt.Sprintf(" | %d binary file(s), not counted as LOC", s.Binary)
	}
	return result + "\n"
}
