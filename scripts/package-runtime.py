"""Download and verify the exact upstream zapret2 runtime; no private profiles."""
import argparse, hashlib, json, pathlib, shutil, urllib.request, zipfile
p=argparse.ArgumentParser()
p.add_argument('--output',required=True,type=pathlib.Path)
p.add_argument('--cache',required=True,type=pathlib.Path)
a=p.parse_args()
root=pathlib.Path(__file__).resolve().parents[1]
lock=json.loads((root/'components.lock.json').read_text(encoding='utf-8'))['zapret2']
a.cache.mkdir(parents=True,exist_ok=True)
archive=a.cache/'zapret2-v1.0.5.2.zip'
if not archive.exists():
    with urllib.request.urlopen(lock['url'],timeout=60) as response, archive.open('wb') as f:
        shutil.copyfileobj(response,f)
if hashlib.file_digest(archive.open('rb'),'sha256').hexdigest()!=lock['sha256']:
    raise SystemExit('zapret2 archive checksum mismatch')
a.output.mkdir(parents=True,exist_ok=True)
prefix='zapret2-v1.0.5.2/'
with zipfile.ZipFile(archive) as z:
    for name in z.namelist():
        mapping=None
        if name.startswith(prefix+'binaries/windows-x86_64/') and not name.endswith('/'):
            mapping='zapret/'+name.rsplit('/',1)[1]
        elif name.startswith(prefix+'lua/') and not name.endswith('/'):
            mapping='zapret/lua/'+name[len(prefix+'lua/'):]
        if mapping:
            target=a.output/mapping
            target.parent.mkdir(parents=True,exist_ok=True)
            target.write_bytes(z.read(name))
    for name in z.namelist():
        if name.startswith(prefix) and 'license' in name.lower() and not name.endswith('/'):
            target=a.output/'licenses'/name[len(prefix):].replace('/','__')
            target.parent.mkdir(parents=True,exist_ok=True)
            target.write_bytes(z.read(name))
manifest={p.relative_to(a.output).as_posix():hashlib.file_digest(p.open('rb'),'sha256').hexdigest() for p in a.output.rglob('*') if p.is_file() and p.name!='manifest.json'}
(a.output/'manifest.json').write_text(json.dumps(manifest,indent=2),encoding='utf-8')
print(f'Verified runtime: {len(manifest)} files')
