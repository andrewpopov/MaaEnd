"""Launch the installed C++ agent with MaaFramework DLLs available to Windows."""
import os
from pathlib import Path
import subprocess
import sys

runtime=Path(__file__).resolve().parents[1]/"install"
environment=os.environ.copy()
environment["PATH"]=str(runtime/"maafw")+os.pathsep+environment.get("PATH","")
raise SystemExit(subprocess.call([str(runtime/"agent/cpp-algo.exe"),*sys.argv[1:]],cwd=runtime,env=environment))
