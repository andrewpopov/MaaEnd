"""Merge our fork root, build local edits, and install with a recoverable backup.

Run with the repository Python: python tools/update_local.py --apply --close-app
Omit --apply to build and validate a package without replacing the running app.
"""
import argparse
import base64
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time
import urllib.request

try:
    from .install_runtime import copy_agent_runtime
except ImportError:
    from install_runtime import copy_agent_runtime

PACKAGED_TOOLS = ('account_scan.py', 'gear_workflow.py', 'team_manager.py', 'combat_team.py', 'rotation_readiness.py')


def run(root, *args, env=None):
    print('+ ' + subprocess.list2cmdline(list(map(str,args))), flush=True)
    return subprocess.run(list(map(str,args)), cwd=root, check=True, env=env)


def native_go_environment(root):
    # FFI runtime compatibility is tied to the project's declared compiler.
    # A newer host compiler can pass unit tests yet crash the live agent.
    module=(root/'agent/go-service/go.mod').read_text(encoding='utf8')
    version=next(line.split()[1] for line in module.splitlines() if line.startswith('go '))
    return {**os.environ, 'GOTOOLCHAIN':'go'+version}


def git(root, *args):
    return subprocess.check_output(['git','-C',str(root),*args],text=True,encoding='utf-8').strip()


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream,'sha256').hexdigest()


def inputs(root):
    paths = []
    for directory in ('assets', 'agent/go-service', 'tools/combat_reference', 'tools/setup'):
        for path in (root / directory).rglob('*'):
            if not path.is_file() or any(part in ('__pycache__', 'build') for part in path.parts):
                continue
            if directory == 'agent/go-service' and path.suffix == '.exe':
                continue
            paths.append(path)
    for name in (*PACKAGED_TOOLS, 'fill_interface.py', 'install_runtime.py', 'update_local.py', 'Install-Layered.ps1'):
        path = root / 'tools' / name
        if path.is_file(): paths.append(path)
    # These installed binaries are reused in the staged package.
    for directory in ('maafw', 'agent'):
        for path in (root / 'install' / directory).rglob('*'):
            if path.is_file() and (directory == 'maafw' or path.suffix.lower() == '.dll' or path.name == 'cpp-algo.exe'):
                paths.append(path)
    for name in ('MaaEnd.exe', 'WebView2Loader.dll'):
        path = root / 'install' / name
        if path.is_file(): paths.append(path)
    return {p.relative_to(root).as_posix(): digest(p) for p in sorted(set(paths))}


def api(path, body=None):
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
    request=urllib.request.Request('http://127.0.0.1:12701/api'+path,
        data=None if body is None else json.dumps(body).encode(),headers={'Content-Type':'application/json'})
    return json.load(opener.open(request,timeout=15))


def idle():
    try:state=api('/maa/state')
    except urllib.error.URLError:
        return None
    active=[key for key,value in state['instances'].items() if value.get('is_running')]
    if active:raise RuntimeError('Running MaaEnd tasks must finish before installing: '+', '.join(active))
    return state


def powershell(script):
    encoded=base64.b64encode(script.encode('utf-16le')).decode('ascii')
    run(Path.cwd(),'pwsh','-NoProfile','-EncodedCommand',encoded)


def quote(value):return "'"+str(value).replace("'","''")+"'"


def close_app(install):
    state=idle()
    verified_api = False
    if state:
        interface = api('/interface')
        if Path(interface['basePath']).resolve() != install.resolve():
            raise RuntimeError('The MaaEnd API belongs to another installation; no agents were stopped')
        verified_api = True
        for key in state['instances']:
            api('/maa/instances/'+key+'/agent/stop',{})
    # Only this installation is eligible. Never stop the separate MAA app.
    powershell("$ErrorActionPreference='Stop'; $verifiedApi="+('$true' if verified_api else '$false')+"; $target="+quote(install/'MaaEnd.exe')+r""";
$apiOwner = $null
if ($verifiedApi) { $apiOwner = Get-NetTCPConnection -LocalPort 12701 -State Listen -ErrorAction Stop | Select-Object -First 1 -ExpandProperty OwningProcess }
foreach($p in Get-Process MaaEnd -ErrorAction SilentlyContinue){
 $matchesTarget = if ($p.Path) { [IO.Path]::GetFullPath($p.Path) -ieq $target } else { $verifiedApi -and $p.Id -eq $apiOwner }
 if(-not $p.Path -and -not $matchesTarget){throw 'Cannot verify MaaEnd process path. Exit it manually and resume the validated package.'}
 if($matchesTarget){
  if(-not $p.CloseMainWindow()){throw 'MaaEnd did not accept a normal close request'}
  if(-not $p.WaitForExit(20000)){throw 'MaaEnd is still open. Use Exit from its tray menu, then resume the validated package; installation was not started'}
 }
}
""")


def load_json(path):
    text=path.read_text(encoding='utf-8-sig')
    try:return json.loads(text)
    except json.JSONDecodeError:
        import json5
        return json5.loads(text)



def sync_source(root, branch, skip_fetch=False, local_only=False):
    if local_only:
        print('Building the current working tree; fork synchronization skipped.', flush=True)
        return
    if not skip_fetch:
        run(root, 'git', 'fetch', '--no-recurse-submodules', 'origin', f'refs/heads/{branch}:refs/remotes/origin/{branch}')
    run(root, 'git', 'merge', '--no-edit', f'origin/{branch}')


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--apply',action='store_true')
    parser.add_argument('--close-app',action='store_true')
    parser.add_argument('--skip-fetch',action='store_true')
    parser.add_argument('--local-only',action='store_true',help='Install the current checkout without fetching or merging the fork')
    parser.add_argument('--branch',default='v2')
    parser.add_argument('--resume',type=Path,help='Install a previously validated package manifest')
    args=parser.parse_args()
    root=Path(__file__).resolve().parents[1];install=root/'install'
    if not args.resume:
        sync_source(root, args.branch, args.skip_fetch, args.local_only)
        stamp=dt.datetime.now().strftime('%Y%m%d-%H%M%S-%f');stage=root/'.cache/local-update'/stamp
        package=stage/'package';package.mkdir(parents=True)
        commit=git(root,'rev-parse','HEAD');before=inputs(root)
        manifest={'source_commit':commit,'source_inputs':before,'package':str(package),'installed':False,'validated':False}
        manifest_path=stage/'update.json'
        manifest_path.write_text(json.dumps(manifest,indent=2)+'\n')
        # Stage source resources separately. No build step writes to install/.
        for item in (root/'assets').iterdir():
            if item.is_dir():shutil.copytree(item,package/item.name)
            else:shutil.copy2(item,package/item.name)
        for resource in package.glob('resource*'):
            pipeline=resource/'pipeline'
            if not pipeline.is_dir():continue
            merged={}
            for file in sorted(pipeline.rglob('*.json')):
                doc=load_json(file)
                collisions=set(doc)&set(merged)
                if collisions:raise RuntimeError('Duplicate pipeline nodes: '+str(sorted(collisions)))
                merged.update(doc)
            # Remove only staged JSON sources inside this new package.
            for file in pipeline.rglob('*.json'):file.unlink()
            (pipeline/'nodes.json').write_text(json.dumps(merged,ensure_ascii=False,indent=4)+'\n',encoding='utf-8')
        for relative in ['maafw','MaaEnd.exe','agent/cpp-algo.exe']:
            src=install/relative;dst=package/relative;dst.parent.mkdir(parents=True,exist_ok=True)
            if src.is_dir():shutil.copytree(src,dst)
            else:shutil.copy2(src,dst)
        copy_agent_runtime(install, package)
        for filename in ['README.md','LICENSE']:shutil.copy2(root/filename,package/filename)
        tool_dest=package/'tools';tool_dest.mkdir()
        for filename in PACKAGED_TOOLS:
            shutil.copy2(root/'tools'/filename,tool_dest/filename)
        shutil.copytree(root/'tools/combat_reference',tool_dest/'combat_reference')
        run(root,'go','-C',root/'agent/go-service','test','./accountscan','./autofight','./innkeeping',env=native_go_environment(root))
        version='fork-'+commit[:8]+'-local-'+stamp
        run(root,'go','-C',root/'agent/go-service','build','-trimpath','-ldflags=-X main.Version='+version,'-o',package/'agent/go-service.exe','.',env=native_go_environment(root))
        run(root,sys.executable,root/'tools/fill_interface.py','--version',version,'--maafw-lib-dir',install/'maafw','--interface',package/'interface.json')
        if before!=inputs(root):raise RuntimeError('Source changed during build. Re-run to build a consistent snapshot.')
        run(root,sys.executable,root/'tools/install_runtime.py','--package',package,'--output',stage/'native-validation.json')
        if before!=inputs(root):raise RuntimeError('Packaged inputs changed during native validation. Build a fresh package.')
        manifest.update(validated=True,version=version,package_hashes={p.relative_to(package).as_posix():digest(p) for p in package.rglob('*') if p.is_file()})
        manifest_path.write_text(json.dumps(manifest,indent=2)+'\n')
    else:
        manifest_path=args.resume.resolve();manifest=load_json(manifest_path);package=Path(manifest['package'])
        if not manifest.get('validated') or manifest.get('installed'):raise RuntimeError('Resume requires an uninstalled validated package')
        if manifest['source_inputs']!=inputs(root):raise RuntimeError('Source changed since staging; build a fresh package')
        for relative,expected in manifest['package_hashes'].items():
            if digest(package/relative)!=expected:raise RuntimeError('Staged package changed: '+relative)
    if args.resume:
        run(root,sys.executable,root/'tools/install_runtime.py','--package',package,'--output',manifest_path.parent/'native-validation.json')
    print('Validated package: '+str(manifest_path),flush=True)
    if not args.apply:return
    idle()
    if args.close_app:close_app(install)
    protected=[install/'data/AccountScans/account_scans.sqlite3',install/'EssenceInventory.json',install/'data/AutoCollect/history.json']
    protected.extend(p for p in (install/'config').rglob('*') if p.is_file())
    hashes={str(p):digest(p) for p in protected if p.is_file()}
    powershell('& '+quote(root/'tools/Install-Layered.ps1')+' -PackagePath '+quote(package)+' -GuiPath '+quote(package/'MaaEnd.exe')+' -InstallPath '+quote(install)+' -SourceCommit '+quote(manifest['source_commit'])+' -Apply')
    for filename,expected in hashes.items():
        if digest(Path(filename))!=expected:raise RuntimeError('Preserved account file changed: '+filename)
    for relative,expected in manifest['package_hashes'].items():
        if digest(install/relative)!=expected:raise RuntimeError('Installed package file differs: '+relative)
    manifest.update(installed=True,preserved_hashes=hashes,installed_at=dt.datetime.now(dt.timezone.utc).isoformat())
    manifest_path.write_text(json.dumps(manifest,indent=2)+'\n')
    print('Installed and verified: '+str(manifest_path),flush=True)


if __name__=='__main__':main()
