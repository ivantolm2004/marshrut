"""Collect root license/notice files for downloaded, pinned Go modules."""
import argparse, json, pathlib, shutil, stat, subprocess
p = argparse.ArgumentParser()
p.add_argument('--go', default='go')
a = p.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
raw = subprocess.check_output([a.go, 'list', '-mod=mod', '-m', '-json', 'all'], cwd=root / 'core', text=True)
decoder = json.JSONDecoder(); modules = []; offset = 0
while raw[offset:].strip():
    offset += len(raw[offset:]) - len(raw[offset:].lstrip())
    module, length = decoder.raw_decode(raw, offset); offset = length
    if module.get('Main'): continue
    source = module.get('Replace', module)
    directory = pathlib.Path(source['Dir']) if 'Dir' in source else None
    saved = []
    if directory:
        target = root / 'licenses/go' / (module['Path'].replace('/', '__') + '@' + module.get('Version', 'local'))
        for file in directory.iterdir():
            if file.is_file() and file.name.lower().startswith(('license', 'copying', 'notice', 'copyright')):
                target.mkdir(parents=True, exist_ok=True)
                destination = target / file.name
                if destination.exists(): destination.chmod(destination.stat().st_mode | stat.S_IWRITE)
                shutil.copyfile(file, destination); saved.append(file.name)
    modules.append({'module': module['Path'], 'version': module.get('Version', ''), 'notices': saved})
(root / 'licenses/go-modules.json').write_text(json.dumps(modules, indent=2), encoding='utf-8')
print(f'Collected notices for {sum(bool(m["notices"]) for m in modules)} Go modules.')
