#ifndef SourceDir
  #error SourceDir must point to the verified Flutter release bundle
#endif
#ifndef OutputDir
  #define OutputDir "..\..\release"
#endif
[Setup]
AppId={{2B53D9B4-96B7-4B3E-AF1A-B7A85C942527}
AppName=Маршрут
AppVersion=0.1.0
DefaultDirName={autopf}\Marshrut
DefaultGroupName=Маршрут
OutputDir={#OutputDir}
OutputBaseFilename=Marshrut-0.1.0-Windows-x64-Setup
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0.17763
PrivilegesRequired=admin
Compression=lzma2
SolidCompression=yes
UninstallDisplayIcon={app}\marshrut.exe
WizardStyle=modern
[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "install-service.ps1"; DestDir: "{app}"; Flags: ignoreversion
Source: "uninstall-service.ps1"; DestDir: "{app}"; Flags: ignoreversion
[Icons]
Name: "{group}\Маршрут"; Filename: "{app}\marshrut.exe"
[Run]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\install-service.ps1"" -InstallDirectory ""{app}"""; Flags: runhidden waituntilterminated
Filename: "{app}\marshrut.exe"; Description: "Открыть Маршрут"; Flags: nowait postinstall skipifsilent runasoriginaluser
[UninstallRun]
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; Parameters: "-NoProfile -ExecutionPolicy Bypass -File ""{app}\uninstall-service.ps1"""; Flags: runhidden waituntilterminated
