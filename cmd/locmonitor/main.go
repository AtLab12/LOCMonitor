package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"locmonitor/internal/monitor"
)

var version = "dev"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 {
		args = []string{"status"}
	}
	var err error
	switch args[0] {
	case "--version", "version":
		fmt.Println("locmonitor " + version)
	case "--help", "help", "-h":
		fmt.Print(help)
	case "status":
		jsonOutput, check := false, false
		for _, arg := range args[1:] {
			switch arg {
			case "--json":
				jsonOutput = true
			case "--check":
				check = true
			default:
				return fail(fmt.Errorf("unknown status option: %s", arg))
			}
		}
		var s monitor.Stats
		s, err = monitor.Measure("")
		if errors.Is(err, monitor.ErrNotMonitored) {
			if jsonOutput {
				fmt.Println(`{"monitored":false}`)
			}
			return 0
		}
		if err == nil {
			if jsonOutput {
				err = json.NewEncoder(os.Stdout).Encode(s)
			} else {
				err = monitor.Display(os.Stdout, s)
			}
			if err == nil && check && s.Total > s.Limit {
				return 1
			}
		}
	case "init":
		dir, migrate := "", false
		for _, arg := range args[1:] {
			if arg == "--migrate" {
				migrate = true
			} else if dir == "" && !strings.HasPrefix(arg, "-") {
				dir = arg
			} else {
				return fail(errors.New("usage: locmonitor init [repository-path] [--migrate]"))
			}
		}
		err = monitor.Init(dir, migrate)
		if err == nil {
			filename, _ := monitor.ConfigPath()
			fmt.Println("Repository registered in " + filename)
			if migrate {
				fmt.Println("Imported legacy settings. The old .locmonitor.json is no longer read and may be removed.")
			}
		}
	case "config":
		if len(args) != 2 || (args[1] != "path" && args[1] != "init") {
			return fail(errors.New("usage: locmonitor config path|init"))
		}
		if args[1] == "init" {
			err = monitor.CreateConfig()
		}
		if err == nil {
			var filename string
			filename, err = monitor.ConfigPath()
			if err == nil {
				fmt.Println(filename)
			}
		}
	case "shell-init":
		if len(args) != 2 {
			return fail(errors.New("usage: locmonitor shell-init bash|zsh|fish"))
		}
		switch args[1] {
		case "bash", "zsh":
			fmt.Print("git() { command locmonitor git \"$@\"; }\n")
		case "fish":
			fmt.Print("function git\n  command locmonitor git $argv\nend\n")
		default:
			err = errors.New("supported shells: bash, zsh, fish")
		}
	case "git":
		return wrapGit(args[1:])
	case "hook":
		if len(args) != 2 {
			return fail(errors.New("usage: locmonitor hook install|uninstall"))
		}
		err = hook(args[1])
	default:
		err = fmt.Errorf("unknown command %q; use locmonitor --help", args[0])
	}
	if err != nil {
		return fail(err)
	}
	return 0
}

func fail(err error) int { fmt.Fprintln(os.Stderr, "locmonitor:", err); return 2 }

// Only recognize Git's actual subcommand, never a path or a commit message.
func commitPrefix(args []string) ([]string, bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) && args[i+1] == "commit" {
				return args[:i], true
			}
			return nil, false
		}
		if !strings.HasPrefix(arg, "-") {
			return args[:i], arg == "commit"
		}
		switch arg {
		case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env":
			i++
		case "--help", "-h", "--version", "-v":
			return nil, false
		}
	}
	return nil, false
}

// Recognize no-op amendments even when Git produces the same commit hash.
// Skip option values so a message such as "--dry-run" is still just a message.
func realAmend(args []string) bool {
	amend, preview := false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--":
			return amend && !preview
		case "--amend":
			amend = true
		case "--dry-run", "--short", "--porcelain", "--long", "--help", "-h":
			preview = true
		case "-m", "--message", "-F", "--file", "-C", "--reuse-message", "-c", "--reedit-message", "--fixup", "--squash", "--author", "--date", "-t", "--template", "--trailer", "--pathspec-from-file":
			i++
		}
	}
	return amend && !preview
}

func wrapGit(args []string) int {
	prefix, isCommit := commitPrefix(args)
	var root string
	var before string
	if isCommit {
		p := append(append([]string{}, prefix...), "rev-parse", "--show-toplevel")
		if b, err := monitor.Git("", p...); err == nil {
			root = strings.TrimSpace(string(b))
		}
		p = append(append([]string{}, prefix...), "rev-parse", "--verify", "HEAD")
		if b, err := monitor.Git("", p...); err == nil {
			before = strings.TrimSpace(string(b))
		}
	}
	cmd := exec.Command("git", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Avoid duplicate output when both integrations are installed.
	if isCommit {
		cmd.Env = append(os.Environ(), "LOCMONITOR_WRAPPED=1")
	}
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if exit.ExitCode() < 0 {
				return 1
			}
			return exit.ExitCode()
		}
		return fail(err)
	}
	if isCommit && root != "" {
		p := append(append([]string{}, prefix...), "rev-parse", "--verify", "HEAD")
		after, err := monitor.Git("", p...)
		// --dry-run and --help can succeed without creating a commit.
		if err == nil && (strings.TrimSpace(string(after)) != before || realAmend(args[len(prefix)+1:])) {
			s, err := monitor.Measure(root)
			if errors.Is(err, monitor.ErrNotMonitored) {
				return 0
			} else if err != nil {
				fmt.Fprintln(os.Stderr, "locmonitor: commit succeeded;", err)
			} else {
				if err := monitor.Display(os.Stdout, s); err != nil {
					fmt.Fprintln(os.Stderr, "locmonitor: commit succeeded;", err)
				}
			}
		}
	}
	return 0
}

const hookBody = `#!/bin/sh
# Managed by LOCMonitor. Use locmonitor hook uninstall to remove.
if [ "$LOCMONITOR_WRAPPED" != "1" ] && command -v locmonitor >/dev/null 2>&1; then
  locmonitor status || :
fi
exit 0
`

func hook(action string) error {
	if action != "install" && action != "uninstall" {
		return errors.New("usage: locmonitor hook install|uninstall")
	}
	root, err := monitor.Root("")
	if err != nil {
		return err
	}
	out, err := monitor.Git(root, "rev-parse", "--git-path", "hooks/post-commit")
	if err != nil {
		return err
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	if action == "uninstall" {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(data) != hookBody {
			return errors.New("hook was not created by LOCMonitor or has been edited; leaving it untouched")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		fmt.Println("Removed LOCMonitor post-commit hook.")
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err != nil {
		return fmt.Errorf("cannot install hook (existing hooks are never overwritten): %w; see README for manual integration", err)
	}
	_, writeErr := f.WriteString(hookBody)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	fmt.Println("Installed post-commit hook. Git runs this before its commit summary; use shell-init for output below the summary.")
	return nil
}

const help = `LOCMonitor — keep pull requests small

Usage:
  locmonitor config init           Create the global config with a placeholder
  locmonitor config path           Print the global config path
  locmonitor init [path] [--migrate]
                                   Register a repository; optionally import its old config
  locmonitor status [--json] [--check]
                                   Show committed branch additions + deletions
  locmonitor shell-init bash|zsh|fish
                                   Print shell integration for normal git commit
  locmonitor hook install|uninstall
                                   Optional hook for Git clients (before summary)
  locmonitor git <git arguments>    Run Git, then report after a successful commit
  locmonitor --version

Config: ~/.config/locmonitor/config.json (or XDG_CONFIG_HOME / LOCMONITOR_CONFIG).
Only listed repositories are monitored. Unlisted repositories produce no report.
Default: 400 LOC, warning at 80%, base auto-detected from origin/HEAD, main, master.
--check exits 1 above the budget; errors exit 2. Commits are never blocked.
`
