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
brew install --HEAD atlab12/locmonitor/locmonitor
```

This builds the latest `main` source with Go (installed automatically by Homebrew). After a stable release formula has been published to `Formula/locmonitor.rb`, omit `--HEAD`. No Homebrew core submission is required.

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

Then, inside a repository:

```sh
locmonitor init
# Edit .locmonitor.json, then commit it to share the settings with your team.
git commit -am "Add a small feature"
```

The shell function delegates to the real Git executable with your arguments and terminal input/output intact. After a successful commit it prints the bar; failed commits keep their exit status and get no bar. A reporting error never changes the success of an actual commit. Other Git commands pass through normally. Git must be installed independently of a manually downloaded binary.

The shell function applies to interactive `git commit` calls, including `git -C path commit` and `git commit --amend`. A shell alias named `git` must be removed before loading the function. Git aliases such as `git ci`, scripts using `command git`, IDEs, and direct executable calls bypass this commit detection. Shell integration is opt-in; installing the binary alone does not modify your shell or repositories.

Without changing your shell, use:

```sh
locmonitor git commit -m "Add a small feature"
```

## Configuration

`.locmonitor.json` lives at the repository root. Every field is optional:

```json
{
  "base": "origin/main",
  "limit": 400,
  "warn_at_percent": 80,
  "exclude": ["**/*.lock", "vendor/**", "generated/**"]
}
```

| Field | Default | Meaning |
| --- | --- | --- |
| `base` | `"auto"` | PR target branch or another commit reference |
| `limit` | `400` | Maximum additions + deletions |
| `warn_at_percent` | `80` | Percentage at which the warning starts |
| `exclude` | `[]` | Repository-relative Git glob patterns to omit |

Automatic base selection tries `origin/HEAD`, `origin/main`, local `main`, `origin/master`, then local `master`. Set `base` explicitly for stacked PRs, a different remote, or another target branch. LOCMonitor does not fetch or contact GitHub. Fetch your target branch yourself to keep the local reference current; shallow clones may need more history.

Exclusions use Git's `glob` pathspec syntax: `*.lock` matches root files; `**/*.lock` matches lockfiles at any depth. No files are excluded automatically. The config file itself counts if committed unless you exclude it.

The display progresses through `OK`, `NEAR LIMIT`, `LIMIT REACHED`, and `OVER LIMIT`. The bar caps at 100%, while the numeric count and percentage keep growing. LOC means added/deleted text lines, including comments and whitespace. Pure renames add no lines when Git recognizes them. Binary files are shown separately because Git cannot assign them meaningful line counts.

Only committed changes count. Staged, unstaged, and untracked changes are omitted. On the base branch itself the count is zero. Merge commits and rebases are measured from the resulting branch state; this is a local estimate of the PR diff, not a live GitHub PR lookup. An empty repository or missing target reports an actionable error instead of silently inventing a count.

## Status and CI

```sh
locmonitor status
locmonitor status --json
locmonitor status --check
```

`--check` exits `1` when the total **exceeds** the budget, `0` at or below the budget, and `2` for configuration/Git errors. Combine it with `--json` for machine-readable results. Ordinary status and commit integrations only warn. CI needs the target branch and its shared history fetched; use a full checkout or fetch those refs explicitly.

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

The release workflow tests the source, builds macOS/Linux binaries for Intel/AMD and ARM, and publishes source/binary archives, SHA-256 checksums, and a stable `locmonitor.rb` formula. Download that formula from the release, replace `Formula/locmonitor.rb`, and commit it to `main` to enable stable `brew install atlab12/locmonitor/locmonitor`. Before publishing the formula, verify with `brew install --build-from-source atlab12/locmonitor/locmonitor` and `brew test atlab12/locmonitor/locmonitor`.

You can also run `make release VERSION=v0.1.0` locally from the tagged checkout. Release output is written to `dist/`.

## License

MIT. See [LICENSE](LICENSE).
