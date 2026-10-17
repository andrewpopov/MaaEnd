"""Offline integration tests for isolated fork syncs and validation gates."""
import contextlib
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import fork_sync as sync


class ForkSyncTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.seed = self.root / "seed"
        self.seed.mkdir()
        self.git(self.seed, "init", "-b", "v2")
        self.identity(self.seed)
        (self.seed / ".gitignore").write_text(".cache/\n")
        (self.seed / "shared.txt").write_text("base\n")
        self.commit(self.seed, "seed")
        self.fork = self.root / "fork.git"
        self.git(self.root, "clone", "--bare", str(self.seed), str(self.fork))
        self.repo = self.root / "checkout"
        self.git(self.root, "clone", str(self.fork), str(self.repo))
        self.identity(self.repo)
        self.upstream = self.root / "upstream"
        self.git(self.root, "clone", str(self.seed), str(self.upstream))
        self.identity(self.upstream)
        self.git(self.repo, "remote", "add", "upstream", str(self.upstream))
        (self.repo / "custom.txt").write_text("our fixes\n")
        self.commit(self.repo, "local fixes")
        self.git(self.repo, "push", "origin", "v2")
        (self.upstream / "improvement.txt").write_text("upstream improvement\n")
        self.commit(self.upstream, "upstream improvement")
        self.original = self.git(self.repo, "rev-parse", "HEAD")
        self.profile = {"base": "v2", "upstream": "upstream", "checks": [
            ([sys.executable, "-c", "from pathlib import Path; assert Path('custom.txt').exists(); assert Path('improvement.txt').exists()"], ".")
        ]}

    def git(self, cwd, *args):
        return subprocess.run(["git", *args], cwd=cwd, check=True, capture_output=True,
                              text=True, encoding="utf-8", errors="replace").stdout.strip()

    def identity(self, repo):
        self.git(repo, "config", "user.name", "Fork sync test")
        self.git(repo, "config", "user.email", "fork-sync@example.invalid")

    def commit(self, repo, title):
        self.git(repo, "add", ".")
        self.git(repo, "commit", "-m", title)

    def prepare(self):
        with patch.dict(sync.PROFILES, {"maaend": self.profile}), \
             patch.object(sync, "github_repo", side_effect=lambda value: value), \
             patch.object(sys, "argv", ["fork_sync.py", "prepare", "--repo", str(self.repo)]), \
             contextlib.redirect_stdout(io.StringIO()):
            sync.main()

    def state(self):
        path = next((self.repo / ".cache/fork-sync").glob("*/run.json"))
        return path, json.loads(path.read_text())

    def test_prepare_merges_and_validates_without_touching_checkout(self):
        self.prepare()
        _, state = self.state()
        self.assertEqual(state["validated_sha"], self.git(Path(state["worktree"]), "rev-parse", "HEAD"))
        self.assertEqual(self.original, self.git(self.repo, "rev-parse", "HEAD"))
        self.assertFalse((self.repo / "improvement.txt").exists())
        self.assertTrue((Path(state["worktree"]) / "improvement.txt").exists())

    def test_conflict_preserves_worktree_and_blocks_publication(self):
        (self.repo / "shared.txt").write_text("custom change\n")
        self.commit(self.repo, "custom shared change")
        self.git(self.repo, "push", "origin", "v2")
        (self.upstream / "shared.txt").write_text("upstream change\n")
        self.commit(self.upstream, "upstream shared change")
        with self.assertRaises(subprocess.CalledProcessError):
            self.prepare()
        _, state = self.state()
        self.assertNotIn("validated_sha", state)
        self.assertIn("<<<<<<<", (Path(state["worktree"]) / "shared.txt").read_text())
        with self.assertRaises(RuntimeError):
            sync.verified(state)

    def test_edit_after_validation_blocks_publication(self):
        self.prepare()
        _, state = self.state()
        work = Path(state["worktree"])
        (work / "custom.txt").write_text("changed after checks\n")
        self.commit(work, "post-validation edit")
        with self.assertRaisesRegex(RuntimeError, "not passed validation"):
            sync.verified(state)

    def test_failed_revalidation_removes_previous_success(self):
        self.prepare()
        path, state = self.state()
        failed_profile = {**self.profile, "checks": [([sys.executable, "-c", "raise SystemExit(4)"], ".")]}
        with patch.dict(sync.PROFILES, {"maaend": failed_profile}), \
             contextlib.redirect_stdout(io.StringIO()), \
             self.assertRaises(subprocess.CalledProcessError):
            sync.validate(path, state)
        self.assertNotIn("validated_sha", json.loads(path.read_text()))

    def test_github_remote_parsing(self):
        self.assertEqual(sync.github_repo("https://github.com/andrewpopov/MaaEnd.git"), "andrewpopov/MaaEnd")
        self.assertEqual(sync.github_repo("git@github.com:MaaEnd/MaaEnd.git"), "MaaEnd/MaaEnd")
        with self.assertRaises(RuntimeError):
            sync.github_repo("file:///tmp/repo")


if __name__ == "__main__":
    unittest.main()
