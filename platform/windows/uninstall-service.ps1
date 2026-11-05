$ErrorActionPreference='Stop'
$service=Get-Service -Name Marshrut -ErrorAction SilentlyContinue
if($service){Stop-Service -Name Marshrut -Force; & sc.exe delete Marshrut | Out-Null; if($LASTEXITCODE -ne 0){throw 'Не удалось удалить службу.'}}
# Retain the encrypted personal profiles for reinstall; never delete unrelated network state.
