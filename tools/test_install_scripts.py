"""Installer regression cases; use temporary fixtures, never a live controller."""
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from tools.update_local import inputs, close_app, native_go_environment
from tools.setup.agent_runtime import REQUIRED_AGENT_DLLS, copy_agent_runtime, check_agent_runtime


class InstallScriptsTests(unittest.TestCase):
    def test_missing_dll_and_mixed_framework_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "agent").mkdir()
            for name in REQUIRED_AGENT_DLLS:
                (root / "agent" / name).write_bytes(b"runtime")
            missing = root / "agent" / "MaaAgentServer.dll"
            missing.unlink()
            with self.assertRaisesRegex(RuntimeError, "MaaAgentServer"):
                check_agent_runtime(root)
            missing.write_bytes(b"old")
            (root / "maafw").mkdir()
            (root / "maafw/MaaAgentServer.dll").write_bytes(b"new")
            with self.assertRaisesRegex(RuntimeError, "versions differ"):
                check_agent_runtime(root)

    def test_framework_runtime_wins_over_stale_agent_copy(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); install = root / "install"; package = root / "package"
            (install / "agent").mkdir(parents=True); (install / "maafw").mkdir()
            for name in REQUIRED_AGENT_DLLS:
                (install / "agent" / name).write_bytes(b"old")
                (install / "maafw" / name).write_bytes(b"current")
            copy_agent_runtime(install, package)
            self.assertTrue(all((package / "agent" / name).read_bytes() == b"current" for name in REQUIRED_AGENT_DLLS))
            copy_agent_runtime(package, package)  # The developer install stages in place.

    def test_only_inputs_used_by_the_package_invalidate_a_build(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            paths = ['assets/interface.json', 'agent/go-service/main.go',
                     'agent/go-service/vendor/ffi/runtime.go', 'tools/combat_team.py',
                     'tools/install_runtime.py', 'tools/setup/agent_runtime.py',
                     'install/agent/cpp-algo.exe', 'install/agent/MaaUtils.dll',
                     'install/maafw/MaaUtils.dll', 'install/MaaEnd.exe']
            for name in paths:
                p = root / name; p.parent.mkdir(parents=True, exist_ok=True); p.write_bytes(b"before")
            baseline = inputs(root)
            trial = root / 'tools/inn_service_trial.py'; trial.write_text('new trial')
            (root / 'agent/go-service/innkeeping.test.exe').write_bytes(b"test build")
            (root / 'install/agent/go-service.exe').write_bytes(b"not reused")
            self.assertEqual(baseline, inputs(root))
            for name in paths:
                with self.subTest(input=name):
                    p = root / name; p.write_bytes(b"after")
                    self.assertNotEqual(baseline, inputs(root))
                    p.write_bytes(b"before")

    def test_other_installation_is_not_stopped(self):
        calls = []
        def api(path, body=None):
            calls.append(path)
            if path == '/maa/state': return {'instances': {'other': {'is_running': False}}}
            if path == '/interface': return {'basePath': 'C:/other-MaaEnd'}
            self.fail('Another installation must not receive stop requests')
        with patch('tools.update_local.api', side_effect=api), patch('tools.update_local.powershell') as shell:
            with self.assertRaisesRegex(RuntimeError, 'another installation'):
                close_app(Path('C:/intended-MaaEnd'))
            shell.assert_not_called()
        self.assertEqual(calls, ['/maa/state', '/interface'])

    def test_native_build_uses_declared_go_version(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); module = root / 'agent/go-service/go.mod'
            module.parent.mkdir(parents=True); module.write_text('module example\n\ngo 1.25.6\n')
            self.assertEqual(native_go_environment(root)['GOTOOLCHAIN'], 'go1.25.6')


if __name__ == '__main__':
    unittest.main()
