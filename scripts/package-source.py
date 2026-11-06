"""Create a source-only archive; credentials and compiled payloads are excluded."""
import argparse, pathlib, zipfile, os
p=argparse.ArgumentParser()
p.add_argument('--output', required=True, type=pathlib.Path)
a=p.parse_args()
root=pathlib.Path(__file__).resolve().parents[1]
skip_dirs={'.git','.dart_tool','build','release','ephemeral','.gradle','.cxx','.idea','state','runtime','vendor','private','__pycache__'}
skip_suffixes={'.exe','.dll','.so','.aar','.apk','.jks','.keystore','.p12','.pfx','.zip','.pdb','.iml','.dpapi','.pyc','.sqlite','.sqlite-journal','.sqlite-wal','.sqlite-shm','.db'}
skip_names={'local.properties','key.properties','.flutter-plugins','.flutter-plugins-dependencies'}
files=[]
for directory,dirs,names in os.walk(root):
    dirs[:]=sorted(d for d in dirs if d not in skip_dirs)
    for name in sorted(names):
        file=pathlib.Path(directory)/name
        if name in skip_names or name=='.env' or name.startswith('.env.') or file.suffix.lower() in skip_suffixes:continue
        if file.resolve()==a.output.resolve():continue
        files.append(file)
a.output.parent.mkdir(parents=True,exist_ok=True)
with zipfile.ZipFile(a.output,'w',zipfile.ZIP_DEFLATED,compresslevel=9) as z:
    for file in files:z.write(file,'Marshrut/'+file.relative_to(root).as_posix())
print(f'Source archive: {len(files)} files, {a.output.stat().st_size} bytes; no binaries, signing keys or local settings.')
