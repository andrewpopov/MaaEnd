"""Stage Windows agent dependencies and validate a package without game inputs."""
import argparse
import ctypes
import json
import os
from pathlib import Path
import subprocess

try:
    from .setup.agent_runtime import copy_agent_runtime, check_agent_runtime, digest
except ImportError:
    from setup.agent_runtime import copy_agent_runtime, check_agent_runtime, digest


def probe_agent(package, runtime, executable, action, recognition=None):
    from maa.agent_client import AgentClient
    from maa.resource import Resource

    resource = Resource()
    client = AgentClient()
    if not client.bind(resource):
        raise RuntimeError("Cannot bind the isolated agent resource")
    if not client.set_timeout(15000):
        raise RuntimeError("Cannot set agent startup timeout")
    env = os.environ.copy()
    env["MAAFW_BINARY_PATH"] = str(package / "maafw")
    env["PI_CONTROLLER"] = '{"name":"PackageValidation","type":"Adb"}'
    # Do not let a developer's DLL path conceal an incomplete package.
    env["PATH"] = os.pathsep.join((str(Path(os.environ["SystemRoot"]) / "System32"), os.environ["SystemRoot"]))
    binary = package / "agent" / executable
    previous = ctypes.windll.kernel32.SetErrorMode(0x8003)
    try:
        with (runtime / (executable + ".log")).open("w", encoding="utf8") as output:
            process = subprocess.Popen([str(binary), client.identifier], cwd=runtime,
                                       env=env, stdout=output, stderr=subprocess.STDOUT,
                                       creationflags=subprocess.CREATE_NO_WINDOW)
            try:
                if not client.connect():
                    code = process.poll()
                    code = "still running" if code is None else hex(code & 0xffffffff)
                    raise RuntimeError(f"{executable} startup/handshake failed ({code}); see {output.name}")
                if action not in client.custom_action_list:
                    raise RuntimeError(f"{executable} did not register {action}")
                if recognition and recognition not in client.custom_recognition_list:
                    raise RuntimeError(f"{executable} did not register {recognition}")
                return {"sha256": digest(binary), "handshake": True,
                        "actions": len(client.custom_action_list), "game_controller_bound": False}
            finally:
                client.disconnect()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.terminate()
                    process.wait(timeout=5)
    finally:
        ctypes.windll.kernel32.SetErrorMode(previous)


def validate_package(package, output):
    if os.name != "nt":
        raise RuntimeError("Windows package validation must run on Windows")
    package = package.resolve()
    output = output.resolve()
    if output.is_relative_to(package):
        raise ValueError("Validation output must be outside the package")
    check_agent_runtime(package)
    for relative in ("MaaEnd.exe", "interface.json", "agent/go-service.exe", "agent/cpp-algo.exe", "maafw/MaaFramework.dll"):
        if not (package / relative).is_file():
            raise RuntimeError("Incomplete package: " + relative)
    from maa.library import Library
    from maa.resource import Resource
    from maa.toolkit import Toolkit
    runtime = output.parent / "native-validation"
    runtime.mkdir(parents=True, exist_ok=True)
    Library.open(package / "maafw")
    Toolkit.init_option(runtime)
    resource = Resource()
    if not resource.use_cpu():
        raise RuntimeError("Cannot initialize CPU recognition for package validation")
    bundles = []
    for name in ("resource", "resource_adb"):
        if not resource.post_bundle(package / name).wait().succeeded:
            raise RuntimeError("Resource bundle failed to load: " + name)
        bundles.append(name)
    agents = {
        "go-service": probe_agent(package, runtime, "go-service.exe", "AutoFightMainAction", "AutoFightEntryRecognition"),
        "cpp-algo": probe_agent(package, runtime, "cpp-algo.exe", "MapNavigateAction"),
    }
    report = {"package": str(package), "resources": bundles, "agents": agents, "game_controller_bound": False}
    output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf8")
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--package", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    print(json.dumps(validate_package(args.package, args.output)))


if __name__ == "__main__":
    main()
