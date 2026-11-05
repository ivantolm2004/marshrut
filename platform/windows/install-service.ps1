param([string]$InstallDirectory = $PSScriptRoot, [string]$OwnerSid)
$ErrorActionPreference='Stop'
$principal=[Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
if(!$principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)){throw 'Запустите установщик от имени администратора.'}
if(!$OwnerSid){$OwnerSid=[Security.Principal.WindowsIdentity]::GetCurrent().User.Value}
[Security.Principal.SecurityIdentifier]::new($OwnerSid) | Out-Null
$InstallDirectory=(Resolve-Path -LiteralPath $InstallDirectory).Path
$executable=Join-Path $InstallDirectory 'marshrut-core.exe'
if(!(Test-Path -LiteralPath $executable)){throw 'Сетевое ядро отсутствует.'}
$state=Join-Path $env:ProgramData 'Marshrut'
New-Item -ItemType Directory -Path $state -Force | Out-Null
& icacls.exe $state /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
if($LASTEXITCODE -ne 0){throw 'Не удалось защитить хранилище службы.'}
$binary='"'+$executable+'" -mode service -owner "'+$OwnerSid+'" -runtime "'+(Join-Path $InstallDirectory 'runtime')+'" -state "'+$state+'"'
$existing=Get-Service -Name Marshrut -ErrorAction SilentlyContinue
if($existing){Stop-Service -Name Marshrut -Force; & sc.exe config Marshrut binPath= $binary | Out-Null}else{New-Service -Name Marshrut -BinaryPathName $binary -DisplayName 'Маршрут — сетевое ядро' -StartupType Automatic | Out-Null}
if($LASTEXITCODE -and $LASTEXITCODE -ne 0){throw 'Не удалось зарегистрировать службу.'}
& sc.exe failure Marshrut reset= 86400 actions= restart/5000/restart/15000 | Out-Null
Start-Service -Name Marshrut
'Служба Маршрут установлена.'
