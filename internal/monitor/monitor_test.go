package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	git(t, dir, "config", "user.name", "Test")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")
	write(t, dir, "app.txt", "one\ntwo\nthree\n")
	commit(t, dir)
	git(t, dir, "checkout", "-b", "feature")
	return dir
}
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := Git(dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}
func write(t *testing.T, dir, path, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}
func commit(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "test")
}
func measure(t *testing.T, dir string) Stats {
	t.Helper()
	s, err := Measure(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBranchDiffUsesMergeBaseAndIgnoresWorktree(t *testing.T) {
	dir := repo(t)
	write(t, dir, "app.txt", "one\nchanged\nthree\nfour\n")
	commit(t, dir)
	write(t, dir, "extra.txt", "extra\n")
	commit(t, dir)
	git(t, dir, "checkout", "main")
	write(t, dir, "base-only.txt", "base\n")
	commit(t, dir)
	git(t, dir, "checkout", "feature")
	write(t, dir, "dirty.txt", "not committed\n")
	git(t, dir, "add", "dirty.txt")
	s := measure(t, dir)
	if s.Added != 3 || s.Deleted != 1 || s.Total != 4 || s.Files != 2 {
		t.Fatalf("unexpected diff: %+v", s)
	}
}
func TestAmendRevertRenameBinaryAndOddPaths(t *testing.T) {
	dir := repo(t)
	git(t, dir, "mv", "app.txt", "renamed\twith\nnewline.txt")
	write(t, dir, "binary.bin", "a\x00b\x00c")
	commit(t, dir)
	s := measure(t, dir)
	if s.Total != 0 || s.Binary != 1 || s.Files != 2 {
		t.Fatalf("rename/binary: %+v", s)
	}
	write(t, dir, "added.txt", "a\nb\n")
	commit(t, dir)
	write(t, dir, "added.txt", "a\nb\nc\n")
	git(t, dir, "add", ".")
	git(t, dir, "commit", "--amend", "--no-edit")
	if s := measure(t, dir); s.Total != 3 {
		t.Fatalf("amend counted history: %+v", s)
	}
	git(t, dir, "revert", "--no-edit", "HEAD")
	if s := measure(t, dir); s.Total != 0 {
		t.Fatalf("reverted changes counted: %+v", s)
	}
}
func TestConfigExclusionsAndBudget(t *testing.T) {
	dir := repo(t)
	c := Config{Base: "main", Limit: 2, WarnAt: 50, Exclude: []string{"*.lock", ConfigName}}
	data, _ := json.Marshal(c)
	write(t, dir, ConfigName, string(data))
	write(t, dir, "deps.lock", "ignore\nme\n")
	write(t, dir, "new.txt", "a\nb\nc\n")
	commit(t, dir)
	s := measure(t, dir)
	if s.Total != 3 || s.State != "OVER LIMIT" || s.Percent != 150 {
		t.Fatalf("config not applied: %+v", s)
	}
	if !strings.Contains(Render(s), "[########################] 3/2 LOC (150%)") {
		t.Fatal(Render(s))
	}
}
func TestConfigValidation(t *testing.T) {
	dir := t.TempDir()
	for _, body := range []string{`{"limit":0}`, `{"limit":-1}`, `{"base":"-bad"}`, `{"warn_at_percent":101}`, `{"typo":1}`, `{} {}`, `{"exclude":[":(all)"]}`} {
		write(t, dir, ConfigName, body)
		if _, err := Load(dir); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
	write(t, dir, ConfigName, `{"limit":100}`)
	c, err := Load(dir)
	if err != nil || c.Base != "auto" {
		t.Fatalf("partial config: %+v %v", c, err)
	}
}
func TestMissingBaseAndUnrelatedHistory(t *testing.T) {
	dir := repo(t)
	git(t, dir, "branch", "-D", "main")
	if _, err := Measure(dir); err == nil {
		t.Fatal("expected missing base error")
	}
	write(t, dir, ConfigName, `{"base":"feature"}`)
	if s := measure(t, dir); s.Total != 0 {
		t.Fatalf("base branch should be empty: %+v", s)
	}
	git(t, dir, "checkout", "--orphan", "unrelated")
	commit(t, dir)
	if _, err := Measure(dir); err == nil {
		t.Fatal("expected unrelated history error")
	}
}
func TestRemoteDefaultBase(t *testing.T) {
	dir := repo(t)
	git(t, dir, "update-ref", "refs/remotes/origin/trunk", "main")
	git(t, dir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	if s := measure(t, dir); s.Base != "refs/remotes/origin/trunk" {
		t.Fatalf("wrong base: %+v", s)
	}
}
func TestInitDoesNotOverwrite(t *testing.T) {
	dir := repo(t)
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	if err := Init(dir); err == nil {
		t.Fatal("overwrote existing config")
	}
}
func TestLinkedWorktree(t *testing.T) {
	dir := repo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, dir, "worktree", "add", "-b", "linked", linked, "main")
	write(t, linked, "new.txt", "a\nb\n")
	commit(t, linked)
	if s := measure(t, linked); s.Total != 2 {
		t.Fatalf("linked worktree: %+v", s)
	}
}

func TestFileTypeAndFolderExclusions(t *testing.T) {
	dir := repo(t)
	files := map[string]string{
		"root.json":                          "ignored\n",
		"src/deep/data.json":                 "ignored\n",
		"deps.lock":                          "ignored\n",
		"src/deep/deps.lock":                 "ignored\n",
		"web/app.min.js":                     "ignored\n",
		"vendor/library.txt":                 "ignored\n",
		"vendor/nested/library.txt":          "ignored\n",
		"generated files/[cache]/output.txt": "ignored\n",
		"src/generated/deep/output.txt":      "ignored\n",
		"vendor/binary.bin":                  "ignored\x00binary",
		"vendor-other/keep.txt":              "keep\n",
		"src/vendor/keep.txt":                "keep\n",
		"generated files/c/output.txt":       "keep\n",
		"web/app.js":                         "keep\n",
		"data.json.backup":                   "keep\n",
		"README.JSON":                        "keep\n",
		"custom.skip":                        "ignored\n",
	}
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0755); err != nil {
			t.Fatal(err)
		}
		write(t, dir, name, body)
	}
	commit(t, dir)
	// Keep the config uncommitted so it does not affect the measured diff.
	write(t, dir, ConfigName, `{
  "exclude_extensions": [".json", "lock", "min.js"],
  "exclude_folders": ["vendor/", "./generated files/[cache]/", "src/generated"],
  "exclude": ["**/*.skip"]
 }`)
	s := measure(t, dir)
	if s.Total != 6 || s.Files != 6 || s.Binary != 0 {
		t.Fatalf("unexpected included files: %+v", s)
	}
	// Running from a subdirectory must use the same repository-relative exclusions.
	if nested := measure(t, filepath.Join(dir, "src", "deep")); nested != s {
		t.Fatalf("subdirectory changed exclusions: %+v", nested)
	}
}

func TestExclusionsAlsoApplyToDeletions(t *testing.T) {
	dir := repo(t)
	git(t, dir, "checkout", "main")
	os.MkdirAll(filepath.Join(dir, "vendor", "nested"), 0755)
	write(t, dir, "data.json", "ignored\n")
	write(t, dir, "vendor/nested/file.txt", "ignored\n")
	commit(t, dir)
	git(t, dir, "checkout", "-b", "delete-files")
	git(t, dir, "rm", "data.json", "vendor/nested/file.txt", "app.txt")
	commit(t, dir)
	write(t, dir, ConfigName, `{"exclude_extensions":["json"],"exclude_folders":["vendor"]}`)
	if s := measure(t, dir); s.Deleted != 3 || s.Added != 0 || s.Files != 1 {
		t.Fatalf("excluded deletions counted: %+v", s)
	}
}

func TestExclusionValidation(t *testing.T) {
	dir := t.TempDir()
	for _, extension := range []string{"", ".", "..json", "json.", "*.json", "src/json", "json\\foo", "[js]", "j?on", "json\x00", " json"} {
		data, _ := json.Marshal(map[string]any{"exclude_extensions": []string{extension}})
		write(t, dir, ConfigName, string(data))
		if _, err := Load(dir); err == nil {
			t.Errorf("accepted extension %q", extension)
		}
	}
	for _, folder := range []string{"", ".", "./", "/vendor", "..", "../vendor", "a/../vendor", "vendor\\nested", "vendor\x00"} {
		data, _ := json.Marshal(map[string]any{"exclude_folders": []string{folder}})
		write(t, dir, ConfigName, string(data))
		if _, err := Load(dir); err == nil {
			t.Errorf("accepted folder %q", folder)
		}
	}
}
