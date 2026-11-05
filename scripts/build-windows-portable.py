"""Build Flutter's C-ABI Win32 runner with a portable LLVM/MinGW toolchain.
No Visual Studio installation, registry changes or elevated build process.
"""
import argparse, os, pathlib, shutil, subprocess
p=argparse.ArgumentParser()
p.add_argument('--flutter-root',required=True,type=pathlib.Path)
p.add_argument('--llvm-root',required=True,type=pathlib.Path)
p.add_argument('--project',type=pathlib.Path,default=pathlib.Path(__file__).resolve().parents[1])
p.add_argument('--skip-assemble',action='store_true')
a=p.parse_args()
root=a.project.resolve();ui=root/'ui';flutter=a.flutter_root.resolve();llvm=a.llvm_root.resolve()
def run(args,cwd=ui):subprocess.run([str(x) for x in args],cwd=cwd,check=True)
if not a.skip_assemble:
    run([flutter/'bin'/'flutter.bat','assemble','--no-version-check','--output=build','-dTargetPlatform=windows-x64','-dBuildMode=release','-dTargetFile=lib/main.dart','-dTreeShakeIcons=true','-dDartObfuscation=false','release_bundle_windows-x64_assets'])
ephemeral=ui/'windows'/'flutter'/'ephemeral'
wrapper=ephemeral/'cpp_client_wrapper'
runner=ui/'windows'/'runner'
bundle=root/'release'/'windows-x64'
bundle.mkdir(parents=True,exist_ok=True)
objects=ui/'build'/'portable-objects';objects.mkdir(parents=True,exist_ok=True)
compiler=llvm/'bin'/'x86_64-w64-mingw32-clang++.exe'
windres=llvm/'bin'/'x86_64-w64-mingw32-windres.exe'
sources=[runner/name for name in ['main.cpp','flutter_window.cpp','utils.cpp','win32_window.cpp']]
sources += [ui/'windows'/'flutter'/'generated_plugin_registrant.cc']
sources += [wrapper/name for name in ['core_implementations.cc','standard_codec.cc','flutter_engine.cc','flutter_view_controller.cc','plugin_registrar.cc']]
includes=[runner,ui/'windows',ephemeral,wrapper/'include']
common=['-std=c++17','-O2','-DNDEBUG','-DUNICODE','-D_UNICODE','-DNOMINMAX','-D_WIN32_WINNT=0x0A00','-include','iterator']
for include in includes:common+=['-I',str(include)]
compiled=[]
for i,source in enumerate(sources):
    target=objects/f'{i}.o'
    run([compiler,*common,'-c',source,'-o',target]);compiled.append(target)
resource=objects/'resource.o'
run([windres,'-D','FLUTTER_VERSION_MAJOR=0','-D','FLUTTER_VERSION_MINOR=1','-D','FLUTTER_VERSION_PATCH=7','-D','FLUTTER_VERSION_BUILD=8','-I',runner,'-i',runner/'Runner.rc','-o',resource])
run([compiler,'-municode','-mwindows','-o',bundle/'marshrut.exe',*compiled,resource,ephemeral/'flutter_windows.dll.lib','-ldwmapi','-lole32','-lshell32','-lshcore','-luuid','-lversion'])
shutil.copy2(ephemeral/'flutter_windows.dll',bundle/'flutter_windows.dll')
data=bundle/'data';data.mkdir(exist_ok=True)
shutil.copy2(ephemeral/'icudtl.dat',data/'icudtl.dat')
shutil.copy2(ui/'build'/'windows'/'app.so',data/'app.so')
shutil.copytree(ui/'build'/'flutter_assets',data/'flutter_assets',dirs_exist_ok=True)
for name in ['libc++.dll','libunwind.dll','libwinpthread-1.dll']:
    file=llvm/'x86_64-w64-mingw32'/'bin'/name
    if file.exists():shutil.copy2(file,bundle/name)
print(f'Native Flutter Windows bundle: {bundle}')
