package monitor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const ConfigName = ".locmonitor.json"

type Config struct {
	Base              string   `json:"base"`
	Limit             int      `json:"limit"`
	WarnAt            int      `json:"warn_at_percent"`
	Exclude           []string `json:"exclude"`
	ExcludeExtensions []string `json:"exclude_extensions"`
	ExcludeFolders    []string `json:"exclude_folders"`
}

func Defaults() Config {
	return Config{Base: "auto", Limit: 400, WarnAt: 80,
		Exclude: []string{}, ExcludeExtensions: []string{}, ExcludeFolders: []string{}}
}

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

func Load(root string) (Config, error) {
	c := Defaults()
	data, err := os.ReadFile(filepath.Join(root, ConfigName))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("%s: %w", ConfigName, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return c, fmt.Errorf("%s must contain one JSON object", ConfigName)
	}
	if c.Limit < 1 || c.WarnAt < 1 || c.WarnAt > 100 || c.Base == "" || strings.HasPrefix(c.Base, "-") {
		return c, fmt.Errorf("config requires a base, limit > 0, and warn_at_percent between 1 and 100")
	}
	for _, p := range c.Exclude {
		if p == "" || strings.HasPrefix(p, ":") {
			return c, fmt.Errorf("exclude entries must be nonempty Git glob patterns without pathspec magic")
		}
	}
	for i, extension := range c.ExcludeExtensions {
		extension = strings.TrimPrefix(extension, ".")
		if extension == "" || strings.HasPrefix(extension, ".") || strings.HasSuffix(extension, ".") || strings.ContainsAny(extension, "/\\*?[]\x00 \t\r\n") {
			return c, fmt.Errorf("exclude_extensions entries must be extensions such as .json, lock, or min.js (no paths or wildcards)")
		}
		c.ExcludeExtensions[i] = extension
	}
	for i, folder := range c.ExcludeFolders {
		if folder == "" || path.IsAbs(folder) || strings.ContainsAny(folder, "\\\x00") {
			return c, fmt.Errorf("exclude_folders entries must be repository-relative folder paths using / separators")
		}
		for _, part := range strings.Split(folder, "/") {
			if part == ".." {
				return c, fmt.Errorf("exclude_folders entries cannot contain ..")
			}
		}
		folder = path.Clean(folder)
		if folder == "." {
			return c, fmt.Errorf("exclude_folders must name a folder, not the repository root")
		}
		c.ExcludeFolders[i] = folder
	}
	return c, nil
}

func Init(dir string) error {
	root, err := Root(dir)
	if err != nil {
		return err
	}
	data, _ := json.MarshalIndent(Defaults(), "", "  ")
	f, err := os.OpenFile(filepath.Join(root, ConfigName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

type Stats struct {
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
	return "", fmt.Errorf("cannot find a base branch; set base in %s (and fetch it if needed)", ConfigName)
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

func Render(s Stats) string {
	const width = 24
	fill := int(s.Percent * width / 100)
	if fill > width {
		fill = width
	}
	if fill < 0 {
		fill = 0
	}
	result := fmt.Sprintf("LOCMonitor [%s%s] %d/%d LOC (%.0f%%) | +%d -%d | %s", strings.Repeat("#", fill), strings.Repeat("-", width-fill), s.Total, s.Limit, s.Percent, s.Added, s.Deleted, s.State)
	if s.Binary > 0 {
		result += fmt.Sprintf(" | %d binary file(s), not counted as LOC", s.Binary)
	}
	return result + "\n"
}
