"""Prepare pinned native sources. Does not install anything system-wide."""
import argparse, json, pathlib, shutil, subprocess

parser = argparse.ArgumentParser()
parser.add_argument('--work', required=True, type=pathlib.Path)
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
work = args.work.resolve()
work.mkdir(parents=True, exist_ok=True)

pins = json.loads((root / 'components.lock.json').read_text(encoding='utf-8'))
for name, spec in [('sing-box', pins['sing_box']), ('zapret', pins['tpws'])]:
    dest = work / name
    if not dest.exists():
        dest.mkdir(parents=True)
        subprocess.run(['git', 'init', str(dest)], check=True)
        subprocess.run(['git', '-C', str(dest), 'remote', 'add', 'origin', spec['repository']], check=True)
        subprocess.run(['git', '-C', str(dest), 'fetch', '--depth', '1', 'origin', spec['commit']], check=True)
        subprocess.run(['git', '-C', str(dest), 'checkout', '--detach', 'FETCH_HEAD'], check=True)
    revision = subprocess.check_output(['git', '-C', str(dest), 'rev-parse', 'HEAD'], text=True).strip()
    if revision != spec['commit']:
        raise SystemExit(f'{name}: unexpected revision; do not build unreviewed sources')

# Disable upstream libbox's plaintext diagnostic configuration snapshot. Keys
# must exist only in memory and Android Keystore-encrypted application state.
server = work / 'sing-box' / 'experimental' / 'libbox' / 'command_server.go'
text = server.read_text(encoding='utf-8')
old = '\tsaveConfigSnapshot(configContent)'
new = '\t// Marshrut: credential-bearing configuration snapshots are disabled.'
if old in text:
    server.write_text(text.replace(old, new, 1), encoding='utf-8', newline='\n')
elif new not in text:
    raise SystemExit('libbox snapshot patch does not match the pinned source')

# Protect every actual remote socket before connect(), using SCM_RIGHTS. The
# helper fails closed if the VPN service cannot protect the socket.
connection = work / 'zapret' / 'tpws' / 'tpws_conn.c'
text = connection.read_text(encoding='utf-8')
anchor = '\t// Use NONBLOCK to avoid slow connects affecting the performance of other connections'
guard = '\tif (marshrut_protect(remote_fd) != 0) { close(remote_fd); return -1; }\n'
if guard not in text:
    if anchor not in text:
        raise SystemExit('tpws socket patch does not match the pinned source')
    text = 'extern int marshrut_protect(int fd);\n' + text.replace(anchor, guard + anchor, 1)
    connection.write_text(text, encoding='utf-8', newline='\n')
shutil.copy2(root / 'platform' / 'android' / 'native' / 'marshrut_protect.c', work / 'zapret' / 'tpws' / 'marshrut_protect.c')
print('Pinned native sources prepared; socket protection and snapshot patches applied.')
