"""Exercise the real IPC/DPAPI path without installing services or creating TUN."""
import argparse, ctypes, json, pathlib, re, subprocess, tempfile, time
from ctypes import wintypes
p = argparse.ArgumentParser()
p.add_argument('--core', required=True, type=pathlib.Path)
p.add_argument('--runtime', required=True, type=pathlib.Path)
p.add_argument('--work', required=True, type=pathlib.Path)
a = p.parse_args()
# Never send fixture imports to a running production service's pipe.
kernel = ctypes.WinDLL('kernel32', use_last_error=True)
kernel.WaitNamedPipeW.argtypes = [wintypes.LPCWSTR, wintypes.DWORD]
kernel.WaitNamedPipeW.restype = wintypes.BOOL
exists = kernel.WaitNamedPipeW(r'\\.\pipe\Marshrut.Core.v1', 1)
if exists or ctypes.get_last_error() != 2:
    raise RuntimeError('Production IPC pipe exists; stop the installed Marshrut service before this isolated smoke test.')
sid = re.search(r'S-1-[0-9-]+', subprocess.check_output(['whoami', '/user', '/fo', 'csv', '/nh'], text=True)).group()
a.work.mkdir(parents=True, exist_ok=True)
fixture = 'vless://a7c15317-2e8b-4fa8-9dc0-832b206e023b@proxy.example:443?security=tls&type=tcp#Fixture'
def rpc(action, **args):
    result = subprocess.run([str(a.core.resolve()), '-mode', 'rpc'], input=json.dumps({'action': action, **args}), text=True, encoding='utf-8', capture_output=True, timeout=15)
    data = json.loads(result.stdout)
    if not data.get('ok'): raise RuntimeError('IPC smoke request failed')
    return data.get('data', {})
with tempfile.TemporaryDirectory(prefix='ipc-', dir=a.work) as state:
    for attempt in range(2):
        process = subprocess.Popen([str(a.core.resolve()), '-mode', 'serve', '-owner', sid, '-runtime', str(a.runtime.resolve()), '-state', state], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            status = None
            for _ in range(40):
                if process.poll() is not None: raise RuntimeError('IPC smoke server could not start; ensure the production service is stopped')
                try: status = rpc('status'); break
                except Exception: time.sleep(.1)
            if status is None: raise RuntimeError('IPC smoke server did not respond')
            if attempt == 0: rpc('import', text=fixture)
            assert len(rpc('status')['servers']) == 1
            diagnostics = json.dumps(rpc('diagnostics'))
            assert all(secret not in diagnostics for secret in ['a7c15317-2e8b-4fa8-9dc0-832b206e023b', 'proxy.example', 'vless://', 'Fixture'])
            saved = (pathlib.Path(state) / 'settings.dpapi').read_bytes()
            assert b'proxy.example' not in saved and b'vless://' not in saved
        finally:
            process.terminate(); process.wait(timeout=10)
print('Passed: real named-pipe IPC, DPAPI persistence across restart, redacted diagnostics. No TUN or system routes changed.')
