param(
    [ValidateSet('Core','Windows','Android','All')][string]$Target = 'All',
    [string]$WorkDirectory,
    [string]$Go = 'go',
    [string]$Flutter = 'flutter',
    [string]$Python = 'python',
    [string]$AndroidSdk = $env:ANDROID_HOME,
    [string]$JavaHome = $env:JAVA_HOME,
    [string]$LlvmRoot,
    [string]$AndroidBuildName = '0.1.11',
    [int]$AndroidBuildNumber = 12
)
$ErrorActionPreference = 'Stop'
$project = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
if (!$WorkDirectory) { $WorkDirectory = Join-Path $project '..\..\work\build' }
$WorkDirectory = [IO.Path]::GetFullPath($WorkDirectory)
New-Item -ItemType Directory -Path $WorkDirectory -Force | Out-Null
$env:GOPATH = Join-Path $WorkDirectory 'gopath'
$env:GOBIN = Join-Path $WorkDirectory 'bin'
$env:GOCACHE = Join-Path $WorkDirectory 'go-cache'
$env:GRADLE_USER_HOME = Join-Path $WorkDirectory 'gradle'
$env:PUB_CACHE = Join-Path $WorkDirectory 'pub-cache'
if($JavaHome) { $env:JAVA_HOME = $JavaHome; $env:Path = "$JavaHome\bin;$env:Path" }
if($AndroidSdk) { $env:ANDROID_HOME=$AndroidSdk; $env:ANDROID_SDK_ROOT=$AndroidSdk }
function Run([string]$Command, [string[]]$Arguments) {
    & $Command @Arguments
    if($LASTEXITCODE -ne 0) { throw "Build step failed: $Command" }
}
$tags = 'with_gvisor,with_quic,with_wireguard,with_utls,with_clash_api'
$release = Join-Path $project 'release'
New-Item -ItemType Directory -Path $release -Force | Out-Null
Push-Location (Join-Path $project 'core')
try {
    Run $Go @('test','-mod=mod','-tags',$tags,'./...')
    Run $Go @('build','-mod=mod','-trimpath','-tags',$tags,'-ldflags','-s -w -X github.com/sagernet/sing-box/constant.Version=1.14.3','-o',(Join-Path $release 'marshrut-core.exe'),'./cmd/marshrut-core')
} finally { Pop-Location }
if($Target -eq 'Core') { return }

# Generate normal Flutter platform runners without replacing lib/ or pubspec.
if(!(Test-Path (Join-Path $project 'ui/windows/runner/main.cpp')) -or !(Test-Path (Join-Path $project 'ui/android/gradlew.bat'))) {
    throw 'Platform runners are missing. Restore ui/windows and ui/android from the source package.'
}
Push-Location (Join-Path $project 'ui')
try {
    Run $Flutter @('pub','get')
    Run $Flutter @('analyze')
    Run $Flutter @('test')
} finally { Pop-Location }

if($Target -eq 'Windows' -or $Target -eq 'All') {
    if($LlvmRoot) {
        $flutterRoot = Split-Path (Split-Path (Get-Command $Flutter).Source -Parent) -Parent
        Run $Python @((Join-Path $PSScriptRoot 'build-windows-portable.py'),'--flutter-root',$flutterRoot,'--llvm-root',$LlvmRoot)
        $bundle = Join-Path $release 'windows-x64'
    } else {
        Push-Location (Join-Path $project 'ui')
        try { Run $Flutter @('build','windows','--release') } finally { Pop-Location }
        $bundle = Join-Path $project 'ui\build\windows\x64\runner\Release'
    }
    Run $Python @((Join-Path $PSScriptRoot 'package-runtime.py'),'--output',(Join-Path $release 'runtime'),'--cache',(Join-Path $WorkDirectory 'downloads'))
    Run $Python @((Join-Path $PSScriptRoot 'package-windows.py'),'--bundle',$bundle,'--go',(Get-Command $Go).Source)
}

if($Target -eq 'Android' -or $Target -eq 'All') {
    if(!$AndroidSdk -or !$JavaHome) { throw 'Android SDK and JDK 17 paths are required.' }
    $native = Join-Path $WorkDirectory 'native'
    Run $Python @((Join-Path $PSScriptRoot 'prepare-native.py'),'--work',$native)
    $android = Join-Path $project 'ui\android'
    Run $Python @((Join-Path $PSScriptRoot 'stage-android.py'))
    $libs = Join-Path $android 'app\libs'; New-Item -ItemType Directory -Path $libs -Force | Out-Null
    $binding = Join-Path $WorkDirectory 'binding'; New-Item -ItemType Directory -Path $binding -Force | Out-Null
    # Relative module replacements keep local user/workspace paths out of Go build metadata.
    $bindingUri = [Uri]::new([IO.Path]::GetFullPath($binding) + [IO.Path]::DirectorySeparatorChar)
    $corePath = [Uri]::UnescapeDataString($bindingUri.MakeRelativeUri([Uri]::new((Join-Path $project 'core'))).ToString())
    $boxPath = [Uri]::UnescapeDataString($bindingUri.MakeRelativeUri([Uri]::new((Join-Path $native 'sing-box'))).ToString())
    if (!$corePath.StartsWith('.')) { $corePath = './' + $corePath }
    if (!$boxPath.StartsWith('.')) { $boxPath = './' + $boxPath }
    @"
module marshrut.android.binding
go 1.26.0
require (
 marshrut v0.0.0
 github.com/sagernet/sing-box v1.14.3
)
replace marshrut => "$corePath"
replace github.com/sagernet/sing-box => "$boxPath"
"@ | ForEach-Object { [IO.File]::WriteAllText((Join-Path $binding 'go.mod'), $_, [Text.UTF8Encoding]::new($false)) }
    Run $Go @('install','github.com/sagernet/gomobile/cmd/gomobile@v0.1.12')
    Run $Go @('install','github.com/sagernet/gomobile/cmd/gobind@v0.1.12')
    $env:Path = "$env:GOBIN;$env:Path"
    Push-Location $binding
    try {
        Run (Join-Path $env:GOBIN 'gomobile.exe') @('init')
        $env:GOFLAGS='-mod=mod'
        Run (Join-Path $env:GOBIN 'gomobile.exe') @('bind','-target=android/arm64','-androidapi=29','-javapkg=ru.marshrut','-libname=marshrut','-tags',$tags,'-ldflags','-s -w -checklinkname=0 -X github.com/sagernet/sing-box/constant.Version=1.14.3','-trimpath','-o',(Join-Path $libs 'marshrut.aar'),'marshrut/mobile','github.com/sagernet/sing-box/experimental/libbox')
    } finally { Pop-Location }
    $ndk = Join-Path $AndroidSdk 'ndk\28.2.13676358'
    $compiler = Join-Path $ndk 'toolchains\llvm\prebuilt\windows-x86_64\bin\aarch64-linux-android29-clang.cmd'
    $tpws = Join-Path $native 'zapret\tpws'
    $jni = Join-Path $android 'app\src\main\jniLibs\arm64-v8a'; New-Item -ItemType Directory -Path $jni -Force | Out-Null
    $sources = @((Get-ChildItem -LiteralPath $tpws -Filter '*.c').FullName) + @((Get-ChildItem -LiteralPath (Join-Path $tpws 'andr') -Filter '*.c').FullName)
    Run $compiler (@('-std=gnu99','-O2','-fPIE','-pie','-Wl,-z,max-page-size=16384','-o',(Join-Path $jni 'libtpws.so')) + $sources + @('-lz','-llog'))
    if(!$env:MARSHRUT_KEYSTORE -or !$env:MARSHRUT_KEY_PASSWORD) { throw 'Set MARSHRUT_KEYSTORE and MARSHRUT_KEY_PASSWORD for your personal release signing key.' }
    Push-Location (Join-Path $project 'ui')
    try { Run $Flutter @('build','apk','--release','--target-platform','android-arm64','--build-name',$AndroidBuildName,'--build-number',"$AndroidBuildNumber") } finally { Pop-Location }
    Copy-Item -LiteralPath (Join-Path $project 'ui\build\app\outputs\flutter-apk\app-release.apk') -Destination (Join-Path $release "Marshrut-$AndroidBuildName-arm64.apk") -Force
}
