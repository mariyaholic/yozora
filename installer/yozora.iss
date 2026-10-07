; Per-user installer: no admin prompt, installs to %LocalAppData%\Programs\Yozora.
; Build: iscc /DAppVersion=1.0.0 installer\yozora.iss  (expects Yozora.exe in the repo root)
#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

[Setup]
AppId={{6B0C1D52-5E0A-4C8E-9C57-3F0D7B6A21E4}
AppName=Yozora
AppVersion={#AppVersion}
AppPublisher=Maria
DefaultDirName={localappdata}\Programs\Yozora
DefaultGroupName=Yozora
PrivilegesRequired=lowest
DisableProgramGroupPage=yes
OutputDir=..\dist
OutputBaseFilename=Yozora-Setup
SetupIconFile=..\assets\yozora.ico
UninstallDisplayIcon={app}\Yozora.exe
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; Flags: unchecked

[Files]
Source: "..\Yozora.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Yozora"; Filename: "{app}\Yozora.exe"
Name: "{autodesktop}\Yozora"; Filename: "{app}\Yozora.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\Yozora.exe"; Description: "Open Yozora"; Flags: nowait postinstall skipifsilent

[UninstallRun]
Filename: "{app}\Yozora.exe"; Parameters: "uninstall"; Flags: runhidden; RunOnceId: "RemoveAutostart"
Filename: "{sys}\taskkill.exe"; Parameters: "/F /IM Yozora.exe"; Flags: runhidden; RunOnceId: "StopYozora"
