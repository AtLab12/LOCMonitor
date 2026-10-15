# LOCMonitor

Keep pull requests small. LOCMonitor shows a line budget after each successful `git commit`:

```text
[feature 8e1f304] Add search filters
 3 files changed, 92 insertions(+), 28 deletions(-)
LOCMonitor [#######-----------------] 120/400 LOC (30%) | +92 -28 | OK
```

It measures the **current committed diff for the entire branch**, relative to the merge base with your PR's target branch. Additions and deletions both count: changing one line usually counts as two. It does not sum every commit's diff, so amendments and reverts do not inflate the number.

## Install with Homebrew

The source repository also serves as a custom Homebrew tap:

```sh
brew tap atlab12/locmonitor https://github.com/AtLab12/LOCMonitor.git
brew install atlab12/locmonitor/locmonitor
```

This installs v0.3.0 from a source archive verified with SHA-256, building with Go (installed automatically by Homebrew). No Homebrew core submission is required. Use `brew install --HEAD atlab12/locmonitor/locmonitor` if you prefer the latest `main` source.

To update the development build:

```sh
brew upgrade --fetch-HEAD atlab12/locmonitor/locmonitor
```

Or build locally with Go 1.23+ and Git:

```sh
make build
./bin/locmonitor --help
```

The Go executable has no third-party dependencies. Place `bin/locmonitor` on your `PATH` if building manually.

## Show the bar below Git's commit result

Add this to `~/.zshrc` and reload your shell:

```sh
eval "$(locmonitor shell-init zsh)"
```

For Bash, add `eval "$(locmonitor shell-init bash)"` to `~/.bashrc` (or the startup file your login shell loads). For Fish, add `locmonitor shell-init fish | source` to `~/.config/fish/config.fish`.

Create the global config once:

```sh
locmonitor config init
```

Edit `~/.config/locmonitor/config.json` and replace `"path to your repository"` with a repository path. Or register the current repository automatically:

```sh
locmonitor init
# Now successful commits in this repository show the bar.
git commit -am "Add a small feature"
```

Only explicitly registered repositories are monitored. A missing config, an untouched placeholder, or an unlisted repository produces no LOC report. No per-project config file is created or read.

The shell function delegates to the real Git executable with your arguments and terminal input/output intact. After a successful commit it prints the bar; failed commits keep their exit status and get no bar. A reporting error never changes the success of an actual commit. Other Git commands pass through normally. Git must be installed independently of a manually downloaded binary.

The shell function applies to interactive `git commit` calls, including `git -C path commit` and `git commit --amend`. A shell alias named `git` must be removed before loading the function. Git aliases such as `git ci`, scripts using `command git`, IDEs, and direct executable calls bypass this commit detection. Shell integration is opt-in; installing the binary alone does not modify your shell or repositories.

Without changing your shell, use:

```sh
locmonitor git commit -m "Add a small feature"
```

## Configuration

All repository settings live in **one global file**: `~/.config/locmonitor/config.json` on macOS and Linux. Run `locmonitor config path` to print the actual location. `$XDG_CONFIG_HOME/locmonitor/config.json` takes precedence when `XDG_CONFIG_HOME` is set; `LOCMONITOR_CONFIG=/absolute/path/config.json` overrides both (useful for CI).

`locmonitor config init` creates this structure, with default settings and an inert `"path to your repository"` placeholder. It preserves an existing file. Replace that key with an absolute repository path or a path starting with `~/`, and customize its settings:

```json
{
  "repositories": {
    "path to your repository": {
      "base": "auto",
      "limit": 400,
      "warn_at_percent": 80,
      "exclude_extensions": [".lock", ".svg", "min.js"],
      "exclude_folders": ["vendor", "generated", "docs/assets"],
      "exclude": ["**/*.generated.*"]
    }
  }
}
```

For multiple repositories, add more keys under `repositories`, each with its own settings:

```json
{
  "repositories": {
    "~/Developer/backend": {"base": "origin/main", "limit": 300},
    "~/Developer/frontend": {"limit": 600, "exclude_folders": ["dist"]}
  }
}
```

Paths match exact repository roots, not all repositories under a parent directory. Running Git from a subdirectory still uses its repository's entry. Symlink paths resolve to the same repository. Linked worktrees have separate roots and need separate entries. Relative paths other than `~/...` are rejected; duplicate paths resolving to the same root are rejected too. To stop monitoring a repository, remove its entry.

You can register a path with `locmonitor init /path/to/repository` or run `locmonitor init` inside it. These commands preserve existing settings and other repository entries, and replace the placeholder when registering the first real repository. Each entry independently defaults any omitted fields:

| Field | Default | Meaning |
| --- | --- | --- |
| `base` | `"auto"` | PR target branch or another commit reference |
| `limit` | `400` | Maximum additions + deletions |
| `warn_at_percent` | `80` | Percentage at which the warning starts |
| `exclude_extensions` | `[]` | File extensions to omit everywhere, with or without the leading dot |
| `exclude_folders` | `[]` | Repository-relative folders to omit, including all descendants |
| `exclude` | `[]` | Additional repository-relative Git glob patterns to omit |

Automatic base selection tries `origin/HEAD`, `origin/main`, local `main`, `origin/master`, then local `master`. Set `base` explicitly for stacked PRs, a different remote, or another target branch. LOCMonitor does not fetch or contact GitHub. Fetch your target branch yourself to keep the local reference current; shallow clones may need more history.

All three exclusion lists combine: a file matching any one is omitted from line, file, and binary counts, including when deleted. Matching is case-sensitive. Existing `exclude` rules work unchanged inside each repository entry.

- `exclude_extensions: ["json", ".lock", "min.js"]` excludes those suffixes in the repository root and every nested folder. `min.js` excludes `app.min.js` while keeping `app.js`; `json` does not exclude `data.json.backup`. Use extensions without wildcards.
- `exclude_folders: ["vendor", "src/generated"]` excludes those exact paths and everything beneath them. Paths are relative to the repository root, even when you run Git from a subfolder. A trailing `/` is optional. `vendor` keeps `vendor-other` and `src/vendor`; to exclude every folder named `vendor`, use `exclude: ["**/vendor/**"]`. Spaces and brackets in folder names are treated literally. Use `/` separators, without absolute paths or `..`.

The advanced `exclude` list uses Git's `glob` pathspec syntax: `*.lock` matches root files; `**/*.lock` matches lockfiles at any depth. No files are excluded automatically. The global config lives outside your repositories and is not included in their diffs.

The display progresses through `OK`, `NEAR LIMIT`, `LIMIT REACHED`, and `OVER LIMIT`. The bar caps at 100%, while the numeric count and percentage keep growing. LOC means added/deleted text lines, including comments and whitespace. Pure renames add no lines when Git recognizes them. Binary files are shown separately because Git cannot assign them meaningful line counts.

Only committed changes count. Staged, unstaged, and untracked changes are omitted. On the base branch itself the count is zero. Merge commits and rebases are measured from the resulting branch state; this is a local estimate of the PR diff, not a live GitHub PR lookup. An empty repository or missing target reports an actionable error instead of silently inventing a count.

## Migrating from v0.1 / v0.2

Per-repository `.locmonitor.json` files are no longer read automatically. Run this inside each previously configured repository to copy its settings into the global registry:

```sh
locmonitor init --migrate
```

You can also pass a repository path. Migration validates the old file and refuses to overwrite an existing global entry. It leaves the old file untouched; after checking the global entry, you can remove the old file from the project. No repository scanning or automatic migration occurs.

## Status and CI

```sh
locmonitor status
locmonitor status --json
locmonitor status --check
```

`--check` exits `1` when the total **exceeds** the budget, `0` at or below the budget, and `2` for configuration/Git errors. Combine it with `--json` for machine-readable results. Unlisted repositories exit `0` silently (or return `{"monitored":false}` with `--json`). Ordinary status and commit integrations only warn. CI must supply a global config (for example via `LOCMONITOR_CONFIG`) containing the checkout path; otherwise the repository is unmonitored and the check is skipped. CI also needs the target branch and its shared history fetched; use a full checkout or fetch those refs explicitly.

## Optional Git hook

For IDEs, Git aliases, and other Git clients:

```sh
locmonitor hook install
locmonitor hook uninstall
```

Git runs `post-commit` **before** printing its own commit summary. Use shell integration when placement below the result matters. With both integrations installed, direct `git commit` through the shell wrapper prints only one bar. GUI clients may hide hook output and must have `locmonitor` on their `PATH`.

Hook installation respects `core.hooksPath` (including shared hook directories) and linked worktrees. It never overwrites an existing hook. To integrate with an existing hook manager, add this command to its post-commit configuration:

```sh
if [ "$LOCMONITOR_WRAPPED" != "1" ]; then
  locmonitor status || :
fi
```

Place this before any unconditional `exit` in a shell hook. Uninstall removes only an unmodified hook created by LOCMonitor. Git operations that do not run `post-commit` need an explicit `locmonitor status` afterwards.

To remove shell integration, remove its startup-file line and start a new shell. Uninstall the binary with `brew uninstall locmonitor`.

## Development and releases

```sh
make test
make vet
make build
```

Tests use isolated real Git repositories to check branch diffs, base divergence, amendments, reverts, renames, unusual filenames, binary files, exclusions, linked worktrees, output order, failed commits, hook preservation, and shell integration. CI runs on macOS and Linux.

Maintainers can publish a release by tagging a tested commit and pushing the tag:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The release workflow can also be started manually from the GitHub Actions **Release** page by entering an existing tag (useful if the first tag was pushed before GitHub registered the workflow). It tests the source, builds macOS/Linux binaries for Intel/AMD and ARM, and publishes source/binary archives, SHA-256 checksums, and a stable `locmonitor.rb` formula. Download that formula from the release, replace `Formula/locmonitor.rb`, and commit it to `main` to update the stable `brew install atlab12/locmonitor/locmonitor` version. Before publishing the formula, verify with `brew install --build-from-source atlab12/locmonitor/locmonitor` and `brew test atlab12/locmonitor/locmonitor`.

You can also run `make release VERSION=v0.1.0` locally from the tagged checkout. Release output is written to `dist/`.

## License

MIT. See [LICENSE](LICENSE).
