#!/usr/bin/env python3
"""Prepare, validate, and publish upstream syncs without changing the running app."""
import argparse
import datetime as dt
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys


PROFILES = {
    "maaend": {"base": "v2", "upstream": "upstream", "checks": [
        (["pnpm", "install", "--frozen-lockfile"], "."),
        (["go", "test", "./..."], "agent/go-service"),
        (["pnpm", "check"], "."), (["pnpm", "test"], "."),
    ]},
    "mxu": {"base": "main", "upstream": "upstream", "checks": [
        (["pnpm", "install", "--frozen-lockfile"], "."),
        (["pnpm", "test"], "."), (["pnpm", "build"], "."),
        (["cargo", "test", "--manifest-path", "src-tauri/Cargo.toml"], "."),
    ]},
}


def run(cwd, *args, capture=False):
    executable = shutil.which(str(args[0]))
    if not executable:
        raise RuntimeError(f"Required program is not on PATH: {args[0]}")
    command = [executable, *map(str, args[1:])]
    print("+ " + subprocess.list2cmdline(command), flush=True)
    result = subprocess.run(command, cwd=cwd, check=True, text=True,
                            encoding="utf-8", errors="replace",
                            stdout=subprocess.PIPE if capture else None)
    return result.stdout.strip() if capture else ""


def git(repo, *args):
    return run(repo, "git", *args, capture=True)


def clean(repo):
    if git(repo, "status", "--porcelain", "--untracked-files=no"):
        raise RuntimeError("Tracked files or submodules have changes. Commit them before continuing.")
    if git(repo, "ls-files", "--others", "--exclude-standard"):
        raise RuntimeError("The isolated sync worktree contains untracked files; review them first.")


def save(path, data):
    path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")


def github_repo(url):
    match = re.fullmatch(r"(?:https://github\.com/|git@github\.com:)([^/]+/[^/]+?)(?:\.git)?/?", url)
    if not match:
        raise RuntimeError("Publishing requires a GitHub fork remote.")
    return match.group(1)


def fetch(repo, remote, branch):
    run(repo, "git", "fetch", "--no-recurse-submodules", remote,
        f"refs/heads/{branch}:refs/remotes/{remote}/{branch}")


def validate(path, state):
    work = Path(state["worktree"])
    clean(work)
    # Never publish a tree with an incomplete merge or without upstream ancestry.
    run(work, "git", "merge-base", "--is-ancestor", state["upstream_sha"], "HEAD")
    run(work, "git", "merge-base", "--is-ancestor", state["base_sha"], "HEAD")
    state.pop("validated_sha", None)
    save(path, state)
    for command, directory in PROFILES[state["profile"]]["checks"]:
        run(work / directory, *command)
    clean(work)
    state["validated_sha"] = git(work, "rev-parse", "HEAD")
    state["validated_at"] = dt.datetime.now(dt.timezone.utc).isoformat()
    save(path, state)
    print(f"Validated. Next: python tools/fork_sync.py publish --run {path}")


def verified(state):
    work = Path(state["worktree"])
    clean(work)
    if state.get("validated_sha") != git(work, "rev-parse", "HEAD"):
        raise RuntimeError("This commit has not passed validation. Run validate first.")
    return work


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["check", "prepare", "validate", "publish", "build"])
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--profile", choices=PROFILES, default="maaend")
    parser.add_argument("--base", help="Fork branch and matching upstream branch")
    parser.add_argument("--fork-remote", default="origin")
    parser.add_argument("--upstream-remote")
    parser.add_argument("--run", type=Path, help="Manifest printed by prepare")
    args = parser.parse_args()
    if args.action in {"validate", "publish", "build"}:
        if not args.run:
            parser.error("--run is required")
        path = args.run.resolve()
        state = json.loads(path.read_text(encoding="utf-8"))
        if args.action == "validate":
            validate(path, state)
            return
        work = verified(state)
        fork_url = git(work, "remote", "get-url", state["fork_remote"])
        upstream_url = git(work, "remote", "get-url", state["upstream_remote"])
        if github_repo(fork_url).lower() == github_repo(upstream_url).lower():
            raise RuntimeError("Refusing to publish to the upstream project. Configure a fork remote.")
        fork = github_repo(fork_url)
        if args.action == "publish":
            # Refuse a stale base rather than merging against an old copy of our fork.
            fetch(work, state["fork_remote"], state["base"])
            current_base = git(work, "rev-parse", f'{state["fork_remote"]}/{state["base"]}')
            if current_base != state["base_sha"]:
                raise RuntimeError("Fork base advanced. Prepare a fresh sync before publishing.")
            run(work, "git", "push", state["fork_remote"], f'HEAD:refs/heads/{state["branch"]}')
            prs = json.loads(run(work, "gh", "pr", "list", "--repo", fork,
                "--head", state["branch"], "--base", state["base"], "--json", "url", capture=True))
            if prs:
                print(prs[0]["url"])
            else:
                body = path.with_suffix(".pr.md")
                body.write_text(
                    f'Merge upstream `{state["upstream_sha"]}` into our customized {state["profile"]} fork. '
                    'Retains local commits and upstream submodule pins.\n\n'
                    'Validation: dependency installation and all profile checks passed locally. '
                    'Review remote CI before merging. No installed application files were changed.\n',
                    encoding="utf-8")
                run(work, "gh", "pr", "create", "--repo", fork, "--base", state["base"],
                    "--head", state["branch"], "--title", state["title"], "--body-file", body)
            state["published_sha"] = state["validated_sha"]
            save(path, state)
            return
        if state["profile"] != "maaend":
            raise RuntimeError("Build dispatch currently supports the MaaEnd install workflow only.")
        if state.get("published_sha") != state.get("validated_sha"):
            raise RuntimeError("Publish the validated branch before requesting a build.")
        remote_sha = git(work, "ls-remote", state["fork_remote"], f'refs/heads/{state["branch"]}').split()[0]
        if remote_sha != state["validated_sha"]:
            raise RuntimeError("Published branch changed; refusing to build an unvalidated commit.")
        run(work, "gh", "workflow", "run", "install.yml", "--repo", fork,
            "--ref", state["branch"], "-f", f'ref={state["branch"]}')
        print("CI build requested. Standard artifacts contain STOCK MXU; do not install over our customized GUI.")
        return

    repo = args.repo.resolve()
    profile = PROFILES[args.profile]
    base = args.base or profile["base"]
    upstream = args.upstream_remote or profile["upstream"]
    for name in (base, upstream, args.fork_remote):
        if name.startswith("-"):
            parser.error("Branch and remote names cannot begin with '-'")
    fetch(repo, args.fork_remote, base)
    fetch(repo, upstream, base)
    base_ref, upstream_ref = f"{args.fork_remote}/{base}", f"{upstream}/{base}"
    count = int(git(repo, "rev-list", "--count", f"{base_ref}..{upstream_ref}"))
    print(f"{args.profile}: {count} upstream commits missing from {base_ref}.")
    dirty = git(repo, "status", "--porcelain", "--untracked-files=no")
    if dirty:
        print("Local tracked edits are not included in remote-based syncs. Commit and push them first.")
    if args.action == "check" or not count:
        return
    if dirty:
        raise RuntimeError("Refusing prepare while local tracked changes are uncommitted.")
    if github_repo(git(repo, "remote", "get-url", args.fork_remote)).lower() == github_repo(git(repo, "remote", "get-url", upstream)).lower():
        raise RuntimeError("Fork and upstream remotes point to the same repository.")
    stamp = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%d-%H%M%S-%f")
    folder = repo / ".cache" / "fork-sync" / stamp
    folder.mkdir(parents=True)
    branch = f"codex/sync-{args.profile}-{stamp}"
    work = folder / "worktree"
    state = dict(profile=args.profile, base=base, fork_remote=args.fork_remote,
                 upstream_remote=upstream, worktree=str(work), branch=branch,
                 base_sha=git(repo, "rev-parse", base_ref),
                 upstream_sha=git(repo, "rev-parse", upstream_ref),
                 title=f"chore: sync upstream {base} and submodules")
    path = folder / "run.json"
    save(path, state)
    print(f"Sync manifest: {path}")
    run(repo, "git", "worktree", "add", "-b", branch, work, base_ref)
    try:
        run(work, "git", "merge", "--no-ff", "-m", state["title"], upstream_ref)
    except subprocess.CalledProcessError:
        print(f"Merge stopped for review in {work}. Resolve and commit there, then validate --run {path}")
        raise
    run(work, "git", "submodule", "update", "--init", "--recursive")
    validate(path, state)


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, ValueError, subprocess.CalledProcessError) as exc:
        print(f"Stopped: {exc}", file=sys.stderr)
        sys.exit(1)
