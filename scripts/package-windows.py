"""Package the native Windows app into a self-contained, source-auditable installer."""
import argparse, hashlib, json, pathlib, shutil, stat, subprocess, zipfile
p=argparse.ArgumentParser()
p.add_argument('--bundle',required=True,type=pathlib.Path)
p.add_argument('--go',required=True)
p.add_argument('--output',type=pathlib.Path,help='Optional installer output path')
a=p.parse_args()
root=pathlib.Path(__file__).resolve().parents[1]
release=root/'release';bundle=a.bundle.resolve()
def copy_writable(source, target):
    target=pathlib.Path(target)
    if target.exists():target.chmod(target.stat().st_mode | stat.S_IWRITE)
    result=shutil.copy2(source,target)
    target.chmod(target.stat().st_mode | stat.S_IWRITE)
    return result
required=['marshrut.exe','flutter_windows.dll','data/app.so','data/icudtl.dat','data/flutter_assets/AssetManifest.bin']
for name in required:
    if not (bundle/name).is_file():raise SystemExit(f'Windows bundle incomplete: {name}')
shutil.copy2(release/'marshrut-core.exe',bundle/'marshrut-core.exe')
shutil.copytree(release/'runtime',bundle/'runtime',dirs_exist_ok=True,copy_function=copy_writable)
shutil.copytree(root/'licenses',bundle/'licenses',dirs_exist_ok=True,copy_function=copy_writable)
shutil.copy2(root/'LICENSE',bundle/'LICENSE.txt')
manifest={f.relative_to(bundle).as_posix():hashlib.file_digest(f.open('rb'),'sha256').hexdigest() for f in bundle.rglob('*') if f.is_file() and f.name!='bundle.sha256.json'}
(bundle/'bundle.sha256.json').write_text(json.dumps(manifest,indent=2),encoding='utf-8')
payload=root/'core'/'cmd'/'marshrut-setup'/'payload.zip'
with zipfile.ZipFile(payload,'w',zipfile.ZIP_DEFLATED,compresslevel=6) as z:
    for file in bundle.rglob('*'):
        if file.is_file():z.write(file,file.relative_to(bundle).as_posix())
setup=a.output.resolve() if a.output else release/'Marshrut-0.1.3-Windows-x64-Setup.exe'
subprocess.run([a.go,'build','-mod=mod','-trimpath','-tags','installer','-ldflags','-H=windowsgui -s -w','-o',str(setup),'./cmd/marshrut-setup'],cwd=root/'core',check=True)
print(json.dumps({'installer':setup.name,'bytes':setup.stat().st_size,'sha256':hashlib.file_digest(setup.open('rb'),'sha256').hexdigest()}))
