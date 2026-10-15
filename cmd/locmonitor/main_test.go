package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var testBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "locmonitor-test-bin-")
	if err != nil {
		panic(err)
	}
	testBinary = filepath.Join(dir, "locmonitor")
	cmd := exec.Command("go", "build", "-o", testBinary, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(string(out) + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
func command(t *testing.T, dir, name string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return string(out), e.ExitCode()
		}
		t.Fatal(err)
	}
	return string(out), 0
}
func must(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	out, code := command(t, dir, name, args...)
	if code != 0 {
		t.Fatalf("%s %v: %d %s", name, args, code, out)
	}
	return out
}
func setup(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("PATH", filepath.Dir(testBinary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	dir := t.TempDir()
	must(t, dir, "git", "init", "-b", "main")
	must(t, dir, "git", "config", "user.name", "Test")
	must(t, dir, "git", "config", "user.email", "test@example.com")
	must(t, dir, "git", "config", "commit.gpgsign", "false")
	must(t, dir, "git", "commit", "--allow-empty", "-m", "base")
	must(t, dir, "git", "checkout", "-b", "feature")
	return dir
}
func stage(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("one\ntwo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	must(t, dir, "git", "add", ".")
}
func TestWrapperOutputAndFailures(t *testing.T) {
	dir := setup(t)
	stage(t, dir)
	out := must(t, dir, testBinary, "git", "-C", dir, "commit", "-m", "feature")
	bar := strings.Index(out, "LOCMonitor [")
	summary := strings.Index(out, "2 insertions")
	if summary < 0 || bar < summary || !strings.Contains(out, "2/400 LOC") {
		t.Fatal(out)
	}
	out, code := command(t, dir, testBinary, "git", "commit", "-m", "empty")
	if code == 0 || strings.Contains(out, "LOCMonitor [") {
		t.Fatalf("failed commit: %d %s", code, out)
	}
	os.WriteFile(filepath.Join(dir, "dry-run"), []byte("pending\n"), 0644)
	must(t, dir, "git", "add", ".")
	out = must(t, dir, testBinary, "git", "commit", "--dry-run")
	if strings.Contains(out, "LOCMonitor [") {
		t.Fatal("dry run reported: " + out)
	}
}
func TestHookPreservationAndDuplicateSuppression(t *testing.T) {
	dir := setup(t)
	must(t, dir, testBinary, "hook", "install")
	stage(t, dir)
	out := must(t, dir, testBinary, "git", "commit", "-m", "feature")
	if strings.Count(out, "LOCMonitor [") != 1 {
		t.Fatal(out)
	}
	out = must(t, dir, "git", "commit", "--allow-empty", "-m", "native")
	if !strings.Contains(out, "LOCMonitor [") {
		t.Fatal(out)
	}
	must(t, dir, testBinary, "hook", "uninstall")
	hookPath := filepath.Join(dir, ".git", "hooks", "post-commit")
	os.WriteFile(hookPath, []byte("#!/bin/sh\necho existing\n"), 0755)
	if _, code := command(t, dir, testBinary, "hook", "install"); code == 0 {
		t.Fatal("overwrote existing hook")
	}
	if _, code := command(t, dir, testBinary, "hook", "uninstall"); code == 0 {
		t.Fatal("removed existing hook")
	}
}
func TestConfigErrorDoesNotFailCommit(t *testing.T) {
	dir := setup(t)
	os.WriteFile(filepath.Join(dir, ".locmonitor.json"), []byte(`{"base":"missing"}`), 0644)
	out := must(t, dir, testBinary, "git", "commit", "--allow-empty", "-m", "success")
	if !strings.Contains(out, "commit succeeded;") {
		t.Fatal(out)
	}
}
func TestCheckAndJSON(t *testing.T) {
	dir := setup(t)
	stage(t, dir)
	must(t, dir, "git", "commit", "-m", "feature")
	os.WriteFile(filepath.Join(dir, ".locmonitor.json"), []byte(`{"limit":1}`), 0644)
	out, code := command(t, dir, testBinary, "status", "--json", "--check")
	if code != 1 || !strings.Contains(out, `"total":2`) {
		t.Fatalf("%d %s", code, out)
	}
}
func TestShellIntegration(t *testing.T) {
	dir := setup(t)
	stage(t, dir)
	out := must(t, dir, "bash", "-c", `eval "$(locmonitor shell-init bash)"; git commit -m test`)
	if !strings.Contains(out, "2/400 LOC") {
		t.Fatal(out)
	}
}
func TestCommitPrefix(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"-C", "commit", "status"}, false},
		{[]string{"-c", "x=commit", "commit", "-m", "status"}, true},
		{[]string{"--git-dir", "commit", "status"}, false},
		{[]string{"status"}, false},
		{[]string{"commit", "--amend"}, true},
	} {
		_, got := commitPrefix(tc.args)
		if got != tc.want {
			t.Errorf("%v: %v", tc.args, got)
		}
	}
}

func TestIdenticalAmendAndPreview(t *testing.T) {
	dir := setup(t)
	t.Setenv("GIT_AUTHOR_DATE", "2026-01-01T00:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "2026-01-01T00:00:00Z")
	stage(t, dir)
	must(t, dir, "git", "commit", "-m", "feature")
	before := must(t, dir, "git", "rev-parse", "HEAD")
	out := must(t, dir, testBinary, "git", "commit", "--amend", "--no-edit")
	after := must(t, dir, "git", "rev-parse", "HEAD")
	if before != after || !strings.Contains(out, "LOCMonitor [") {
		t.Fatal(out)
	}
	out = must(t, dir, testBinary, "git", "commit", "--amend", "--dry-run")
	if strings.Contains(out, "LOCMonitor [") {
		t.Fatal(out)
	}
	if !realAmend([]string{"--amend", "-m", "--dry-run"}) {
		t.Fatal("treated message as preview flag")
	}
}

func TestCustomHooksPath(t *testing.T) {
	dir := setup(t)
	must(t, dir, "git", "config", "core.hooksPath", ".custom-hooks")
	must(t, dir, testBinary, "hook", "install")
	if _, err := os.Stat(filepath.Join(dir, ".custom-hooks", "post-commit")); err != nil {
		t.Fatal(err)
	}
	must(t, dir, testBinary, "hook", "uninstall")
}
