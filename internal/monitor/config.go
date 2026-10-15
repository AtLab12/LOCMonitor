package monitor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const LegacyConfigName = ".locmonitor.json"
const PlaceholderPath = "path to your repository"

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

var ErrNotMonitored = errors.New("repository is not monitored; run locmonitor init to register it")

type GlobalConfig struct {
	Repositories map[string]json.RawMessage `json:"repositories"`
}

// ConfigPath uses the same location on macOS and Linux, with explicit overrides
// for XDG environments and isolated CI configurations.
func ConfigPath() (string, error) {
	if value := os.Getenv("LOCMONITOR_CONFIG"); value != "" {
		return filepath.Abs(value)
	}
	if value := os.Getenv("XDG_CONFIG_HOME"); value != "" {
		if !filepath.IsAbs(value) {
			return "", errors.New("XDG_CONFIG_HOME must be an absolute path")
		}
		return filepath.Join(value, "locmonitor", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "locmonitor", "config.json"), nil
}

func decodeObject(data []byte, value any) error {
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("expected a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}

func parseConfig(data []byte) (Config, error) {
	c := Defaults()
	if err := decodeObject(data, &c); err != nil {
		return c, err
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

// Repository keys are exact working-tree roots. Canonicalizing existing paths
// lets a symlink and its target share settings without matching nested repos.
func repositoryPath(value string) (string, error) {
	if strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, value[2:])
	}
	if !filepath.IsAbs(value) || strings.ContainsRune(value, 0) {
		return "", fmt.Errorf("repository path %q must be absolute or start with ~/", value)
	}
	value = filepath.Clean(value)
	canonical, err := filepath.EvalSymlinks(value)
	if err == nil {
		return canonical, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return value, nil
}

func readGlobal(filename string) (GlobalConfig, error) {
	global := GlobalConfig{Repositories: map[string]json.RawMessage{}}
	data, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return global, nil
	}
	if err != nil {
		return global, err
	}
	if err := decodeObject(data, &global); err != nil {
		return global, fmt.Errorf("%s: %w", filename, err)
	}
	if global.Repositories == nil {
		return global, fmt.Errorf("%s: repositories must be an object", filename)
	}
	seen := map[string]bool{}
	for key := range global.Repositories {
		if key == PlaceholderPath {
			continue
		}
		canonical, err := repositoryPath(key)
		if err != nil {
			return global, fmt.Errorf("%s: %w", filename, err)
		}
		if seen[canonical] {
			return global, fmt.Errorf("%s: multiple repository entries resolve to %s", filename, canonical)
		}
		seen[canonical] = true
	}
	return global, nil
}

func Load(root string) (Config, error) {
	filename, err := ConfigPath()
	if err != nil {
		return Config{}, err
	}
	global, err := readGlobal(filename)
	if err != nil {
		return Config{}, err
	}
	canonical, err := repositoryPath(root)
	if err != nil {
		return Config{}, err
	}
	for key, data := range global.Repositories {
		if key == PlaceholderPath {
			continue
		}
		candidate, err := repositoryPath(key)
		if err != nil {
			return Config{}, err
		}
		if candidate == canonical {
			c, err := parseConfig(data)
			if err != nil {
				return c, fmt.Errorf("%s: repository %s: %w", filename, key, err)
			}
			return c, nil
		}
	}
	return Config{}, ErrNotMonitored
}

// Init registers one repository, preserving every existing entry. Migration is
// explicit and never removes the legacy file or changes an existing entry.
func Init(dir string, migrate bool) error {
	root, err := Root(dir)
	if err != nil {
		return err
	}
	root, err = repositoryPath(root)
	if err != nil {
		return err
	}
	return updateGlobal(func(filename string, global GlobalConfig) error {
		for key := range global.Repositories {
			if key == PlaceholderPath {
				continue
			}
			canonical, err := repositoryPath(key)
			if err != nil {
				return err
			}
			if canonical == root {
				if migrate {
					return errors.New("repository already registered; existing settings were left unchanged")
				}
				return nil
			}
		}
		c := Defaults()
		if migrate {
			data, err := os.ReadFile(filepath.Join(root, LegacyConfigName))
			if err != nil {
				return err
			}
			c, err = parseConfig(data)
			if err != nil {
				return fmt.Errorf("%s: %w", LegacyConfigName, err)
			}
		}
		data, err := json.Marshal(c)
		if err != nil {
			return err
		}
		delete(global.Repositories, PlaceholderPath)
		global.Repositories[root] = data
		return writeGlobal(filename, global)
	})
}

// CreateConfig writes an inert example on first setup; it never replaces an
// existing file. The placeholder can be replaced manually or via Init.
func CreateConfig() error {
	return updateGlobal(func(filename string, global GlobalConfig) error {
		if _, err := os.Stat(filename); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		data, err := json.Marshal(Defaults())
		if err != nil {
			return err
		}
		global.Repositories[PlaceholderPath] = data
		return writeGlobal(filename, global)
	})
}

func updateGlobal(update func(string, GlobalConfig) error) error {
	filename, err := ConfigPath()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(filename); err == nil {
		filename = resolved
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filename+".lock", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("cannot lock config %s (another init may be running): %w", filename, err)
	}
	defer os.Remove(filename + ".lock")
	defer lock.Close()
	global, err := readGlobal(filename)
	if err != nil {
		return err
	}
	return update(filename, global)
}

func writeGlobal(filename string, global GlobalConfig) error {
	data, err := json.MarshalIndent(global, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(filename), ".locmonitor-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filename)
}
