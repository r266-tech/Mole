# Incidents

Defects that reached a user or a published release, kept as the eval set for every review layer ("incidents are the
eval", from the Claude Code team's code review write-up, 2026-10-26). A row is added in the same change that fixes
the defect: the fix commit, the commit that introduced it when known, the earliest layer that should have caught it,
and the test or gate that catches it now. `scripts/audit_incidents.py` (run by `scripts/check.sh` and the Security
Checks job) fails a row whose commit does not resolve, whose layer is not one of the names below, or whose catcher is
not defined in the code.

A catcher is written in backticks and is one of: the exact name of a Bats `@test` under `tests/`, a Go `Test*`
function in a `_test.go` file, a Python `test_*` function under `tests/` or `scripts/`, or the repository path of a
gate script (for example `scripts/audit_timeout_status.py`). A Bats name that holds a `|` cannot sit in this table;
rename the test.

Layers, earliest first:

- `write-time`: the author's own check before committing, a sweep of same-shape sites and the source gates, plus the
  `.githooks/pre-commit` hook.
- `check`: `./scripts/check.sh --no-format` (ShellCheck, golangci-lint, the `scripts/audit_*.py` gates), locally and
  in the Check workflow.
- `unit`: the Bats suite through `./scripts/test.sh` and `go test ./...`, locally and in the Validation workflow on
  macos-latest.
- `compat`: the Validation workflow's macos-15 and macos-26 Bats subset, the only lane that sees another macOS
  version's tool output.
- `review`: a diff review, the line-by-line review AGENTS.md requires for destructive sinks, or the weekly audit.
- `e2e`: `scripts/e2e_macos.py` on a real Mac before a release.
- `release`: the release-flow pre-flight and `release.yml` (minimum-OS check, Nix, Homebrew).
- `field`: only a real user's machine, network or configuration shows it.

When a review skill, its prompt or the model running it changes, rerun it on the files each fix touched as they were
at `<Fixed>^` and record which rows it finds (`.claude/skills/weekly-audit`, Phase 3). `Introduced` is a commit only
when it was confirmed by reading the history, otherwise `unknown`.

## Incidents

| ID | Reported | Symptom | Introduced | Fixed | Layer | Caught by |
|---|---|---|---|---|---|---|
| I-01 | 2026-09-20 | Editor obsolete-extension cleanup found nothing on macOS 15, and a nested `.obsolete` key could be offered as an extension directory | 6e70669f | 04cb4947 | compat | `only a root-level .obsolete key marks an extension, never a nested one (#1512)`, `a .obsolete value must be boolean true, not 1 or the string true (#1512)` |
| I-02 | 2026-10-05 | `mo clean` removed project caches that Git tracks, leaving deleted tracked files in the user's clones | unknown | d929d156 | review | `clean_project_caches keeps project caches that Git tracks` |
| I-03 | 2026-10-05 | `mo clean` removed the `~/Library/Logs/DiagnosticReports` directory itself, so macOS could no longer save crash reports | unknown | 4ab50c73 | unit | `clean_user_essentials keeps the DiagnosticReports directory (#1689)` |
| I-04 | 2026-10-18 | `mo update` aborted with a connection error when the GitHub API was rate limited, although install.sh and nightly updates fell back to git tags | unknown | 2e88f475 | review | `stable discovery falls back to git release tags when HTTP API lookups fail` |
| I-05 | 2026-10-18 | Incomplete browser downloads were never found when the home folder name contained a space | cfa0a820 | d822ad0b | unit | `incomplete downloads scan discovers files when HOME contains spaces` |
| I-06 | 2026-10-22 | A shell PATH without `/usr/sbin` read total memory as 0 and refused `mo slim` on Apple Silicon | unknown | b03c0f54 | write-time | `sysctl never resolves through PATH (#1804)`, `get_memory_info reads total memory without /usr/sbin in PATH (#1804)` |
| I-07 | 2026-10-22 | `mo purge` refused every item under a `/System/Volumes/Data` path listed in purge_paths as a critical system path | unknown | 70d320e1 | unit | `validate_path_for_deletion judges a Data volume path by the root path that reaches it (#1814)` |
| I-08 | 2026-10-23 | `mo touchid` reported Touch ID as enabled when the only `pam_tid.so` line was commented out | unknown | 69d2a3c7 | unit | `check_touchid_support ignores a commented-out pam_tid line (#1815)`, `touchid enables the line sudo_local.template ships commented out (#1815)` |
| I-09 | 2026-10-27 | `mo optimize` reported Network Cache Refresh as failed on every run on macOS 27, which refuses `launchctl kill` on mDNSResponder even to root (#1867) | fb920811 | 29b9699d | field | `DNS refresh reports a macOS-refused service signal as skipped without a name fallback` |
| I-10 | 2026-10-27 | Dry-run find scans sized and recorded each candidate twice and returned 1 when no preview ledger was set up (#1865) | 70eed1b0 | a703eddc | review | `safe_find_delete dry run records each target once without removal fallthrough (#1865)`, `safe_sudo_find_delete dry run records targets once and tracks count (#1865)` |
