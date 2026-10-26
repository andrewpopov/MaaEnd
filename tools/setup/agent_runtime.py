"""Windows agent DLL staging shared by developer and local installs."""
import hashlib
import shutil

REQUIRED_AGENT_DLLS = (
    "MaaAgentServer.dll", "MaaUtils.dll", "onnxruntime_maa.dll",
    "opencv_world4_maa.dll", "WebView2Loader.dll",
)


def copy_agent_runtime(install, package):
    target = package / "agent"
    target.mkdir(parents=True, exist_ok=True)
    # Framework DLLs take precedence over stale copies beside the old agent.
    for directory in (install / "agent", install / "maafw"):
        for source in directory.glob("*.dll"):
            destination = target / source.name
            if source.resolve() != destination.resolve():
                shutil.copy2(source, destination)
    webview = install / "WebView2Loader.dll"
    if webview.is_file() and webview.resolve() != (target / webview.name).resolve():
        shutil.copy2(webview, target / webview.name)
    check_agent_runtime(package)


def check_agent_runtime(package):
    for name in REQUIRED_AGENT_DLLS:
        path = package / "agent" / name
        if not path.is_file() or path.stat().st_size == 0:
            raise RuntimeError("Missing or empty C++ agent runtime DLL: " + name)
    for framework in (package / "maafw").glob("*.dll"):
        adjacent = package / "agent" / framework.name
        if adjacent.is_file() and digest(framework) != digest(adjacent):
            raise RuntimeError("Agent/framework runtime DLL versions differ: " + framework.name)


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()

