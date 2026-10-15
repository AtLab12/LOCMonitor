package monitor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestGlobalPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LOCMONITOR_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	p, err := ConfigPath()
	if err != nil || p != filepath.Join(home, ".config", "locmonitor", "config.json") {
		t.Fatalf("%s %v", p, err)
	}
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	p, err = ConfigPath()
	if err != nil || p != filepath.Join(xdg, "locmonitor", "config.json") {
		t.Fatalf("%s %v", p, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "relative")
	if _, err := ConfigPath(); err == nil {
		t.Fatal("accepted relative XDG path")
	}
	override := filepath.Join(t.TempDir(), "custom.json")
	t.Setenv("LOCMONITOR_CONFIG", override)
	p, err = ConfigPath()
	if err != nil || p != override {
		t.Fatalf("%s %v", p, err)
	}
}

func TestPlaceholderAndRegistration(t *testing.T) {
	dir := repo(t)
	filename, _ := ConfigPath()
	os.Remove(filename)
	if err := CreateConfig(); err != nil {
		t.Fatal(err)
	}
	global, err := readGlobal(filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := global.Repositories[PlaceholderPath]; !ok {
		t.Fatal("missing placeholder")
	}
	if _, err := Load(dir); !errors.Is(err, ErrNotMonitored) {
		t.Fatalf("placeholder registered repo: %v", err)
	}
	if err := Init(dir, false); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, dir, `{"limit":123}`)
	if err := CreateConfig(); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil || c.Limit != 123 {
		t.Fatalf("settings lost: %+v %v", c, err)
	}
	global, err = readGlobal(filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := global.Repositories[PlaceholderPath]; ok {
		t.Fatal("placeholder retained after registering repo")
	}
	if _, err := os.Stat(filepath.Join(dir, LegacyConfigName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created per-repo file: %v", err)
	}
}

func TestIndependentRepositoriesAndExactPaths(t *testing.T) {
	first := repo(t)
	second := repo(t)
	// repo() isolates the global config; register the first in the second's registry.
	if err := Init(first, false); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, first, `{"limit":50,"base":"main","exclude_extensions":["json"]}`)
	writeConfig(t, second, `{"limit":900,"base":"feature","exclude_folders":["vendor"]}`)
	a, err := Load(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load(second)
	if err != nil {
		t.Fatal(err)
	}
	if a.Limit != 50 || b.Limit != 900 || a.Base != "main" || b.Base != "feature" || len(a.ExcludeFolders) != 0 || len(b.ExcludeExtensions) != 0 {
		t.Fatalf("settings mixed: %+v %+v", a, b)
	}
	nested := filepath.Join(first, "nested")
	os.MkdirAll(nested, 0755)
	git(t, nested, "init", "-b", "main")
	if _, err := Measure(nested); !errors.Is(err, ErrNotMonitored) {
		t.Fatalf("nested repo inherited parent settings: %v", err)
	}
}

func TestHomeAndSymlinkMatching(t *testing.T) {
	dir := repo(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	alias := filepath.Join(home, "linked repo")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	filename, _ := ConfigPath()
	global := GlobalConfig{Repositories: map[string]json.RawMessage{"~/linked repo/": json.RawMessage(`{"limit":99}`)}}
	if err := writeGlobal(filename, global); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil || c.Limit != 99 {
		t.Fatalf("home/symlink match: %+v %v", c, err)
	}
	if err := Init(dir, false); err != nil {
		t.Fatal(err)
	}
	global, err = readGlobal(filename)
	if err != nil || len(global.Repositories) != 1 {
		t.Fatalf("duplicate registration: %+v %v", global, err)
	}
	canonical, _ := repositoryPath(dir)
	global.Repositories[canonical] = json.RawMessage(`{}`)
	writeGlobal(filename, global)
	if _, err := Load(dir); err == nil {
		t.Fatal("accepted duplicate canonical paths")
	}
}

func TestMigrationAndInvalidConfigPreservation(t *testing.T) {
	dir := repo(t)
	filename, _ := ConfigPath()
	os.Remove(filename)
	legacy := `{"limit":75,"exclude_extensions":[".json"],"exclude_folders":["vendor"]}`
	write(t, dir, LegacyConfigName, legacy)
	if _, err := Load(dir); !errors.Is(err, ErrNotMonitored) {
		t.Fatalf("legacy config activated monitoring: %v", err)
	}
	if err := Init(dir, true); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil || c.Limit != 75 || len(c.ExcludeExtensions) != 1 || len(c.ExcludeFolders) != 1 {
		t.Fatalf("migration: %+v %v", c, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, LegacyConfigName)); err != nil || string(data) != legacy {
		t.Fatal("legacy file modified")
	}
	if err := Init(dir, true); err == nil {
		t.Fatal("migration overwrote registered repository")
	}
	for _, body := range []string{`null`, `{"repositories":null}`, `{"repos":{}}`, `{"repositories":{"relative":{}}}`, `{"repositories":{}} {}`} {
		os.WriteFile(filename, []byte(body), 0600)
		if err := Init(dir, false); err == nil {
			t.Fatalf("accepted invalid global config: %s", body)
		}
		data, _ := os.ReadFile(filename)
		if string(data) != body {
			t.Fatal("invalid config overwritten")
		}
	}
}

func TestConfigLockAndSymlinkPreservation(t *testing.T) {
	dir := repo(t)
	filename, _ := ConfigPath()
	before, _ := os.ReadFile(filename)
	os.WriteFile(filename+".lock", nil, 0600)
	if err := Init(dir, false); err == nil {
		t.Fatal("ignored another writer's lock")
	}
	after, _ := os.ReadFile(filename)
	if string(before) != string(after) {
		t.Fatal("changed locked config")
	}
	os.Remove(filename + ".lock")
	target := filepath.Join(t.TempDir(), "real-config.json")
	os.Rename(filename, target)
	os.Symlink(target, filename)
	// Updating a config reached through a symlink must retain the symlink.
	root, _ := repositoryPath(dir)
	os.WriteFile(target, []byte(`{"repositories":{}}`), 0600)
	if err := Init(dir, false); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(filename); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("config symlink replaced")
	}
	if _, err := Load(root); err != nil {
		t.Fatal(err)
	}
}
