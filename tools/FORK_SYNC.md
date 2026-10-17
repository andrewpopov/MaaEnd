# Keeping our fork current

Run from a shell with Git, Python 3.10+, Go, Node/pnpm and GitHub CLI on PATH.
The installed app and the current checkout are never changed by this script.

On this Windows installation, the launcher finds the existing toolchains:

```powershell
.\tools\Update-Fork.ps1 check
.\tools\Update-Fork.ps1 prepare
```

Or invoke Python directly on any supported platform:

```powershell
python tools/fork_sync.py check
python tools/fork_sync.py prepare
```

`check` fetches the fork and upstream `v2` and reports missing commits. `prepare`
creates a separate `codex/sync-…` branch and worktree under `.cache/fork-sync`,
merges upstream, synchronizes pinned submodules, and runs dependency installation,
Go tests, pipeline checks and screenshot tests. It does not push or merge into `v2`.
Commit and push local fixes first: syncs start from the remote fork branch.

If conflicts or checks fail, the worktree and `run.json` remain for inspection.
Resolve conflicts and commit in that worktree, then rerun validation. Use the
absolute manifest path printed by `prepare` in these commands:

```powershell
python tools/fork_sync.py validate --run '<path-to-run.json>'
python tools/fork_sync.py publish --run '<path-to-run.json>'
python tools/fork_sync.py build --run '<path-to-run.json>'
```

`publish` requires a clean, tested commit and an unchanged fork base, pushes only
the sync branch, and creates or reuses a PR. Review remote CI before merging.
`build` requests the existing MaaEnd `install.yml` workflow for that exact tested
and published branch. It does not wait for, download, or install the artifact.

**The standard CI package downloads stock MXU.** Our pause/resume and agent shutdown
fixes live in the local MXU checkout. Do not install stock CI artifacts over that
customized GUI. A complete custom package must also build our maintained MXU fork
against its compatible MaaFramework version. App installation and idle checks are
intentionally outside this script; configuration and collection history stay put.

The same sync/check engine supports MXU once its local fixes are committed and
pushed to a fork with `origin` pointing to our fork and `upstream` to MistEO/MXU:

```powershell
python tools/fork_sync.py check --profile mxu --repo '..\MXU'
python tools/fork_sync.py prepare --profile mxu --repo '..\MXU'
```

MXU validation runs its tests, frontend build, and Rust tests. MaaFramework updates
come through package builds; keep the MaaUtils submodule compatible with the chosen
runtime. No schedules, automatic merges, force pushes, or app replacements are
created. Run `check` daily, and `prepare` when updates are available. After a PR is
merged, remove its clean worktree with `git worktree remove <path>`; manifests stay
available for reference.
