; Per-user installer: no admin prompt, installs to %LocalAppData%\Programs\Yozora.
; Build: iscc /DAppVersion=1.0.0 installer\yozora.iss  (expects Yozora.exe in the repo root)
#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

[Setup]
AppId={{6B0C1D52-5E0A-4C8E-9C57-3F0D7B6A21E4}
AppName=Yozora
AppVersion={#AppVersion}
VersionInfoVersion={#AppVersion}
UninstallDisplayName=Yozora
AppPublisher=Maria
AppCopyright=Copyright (c) 2026 Maria (mariyaholic)
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
; An upgrade stops Yozora (see PrepareToInstall), so start it again when it was
; running, silent or not. Not after a fresh install, nor when it was not running.
; The single-instance mutex makes a second start (a restart by Windows, or "Open
; Yozora" below) exit quietly.
Filename: "{app}\Yozora.exe"; Parameters: "serve"; Flags: nowait runhidden; Check: RestartAfterUpgrade
Filename: "{app}\Yozora.exe"; Description: "Open Yozora"; Flags: nowait postinstall skipifsilent

[UninstallRun]
; Runs after CurUninstallStepChanged(usUninstall) below has stopped Yozora.
Filename: "{app}\Yozora.exe"; Parameters: "uninstall"; Flags: runhidden; RunOnceId: "RemoveAutostart"

[Code]
const
  CRED_TYPE_GENERIC = 1;

var
  RemoveUserData: Boolean;
  // Yozora was running when Setup began; RestartAfterUpgrade acts on it.
  WasRunning: Boolean;
  RunningAtStart: Boolean;

// The tray's hidden host window exists exactly while a Yozora daemon runs
// (internal/tray trayClassName). The window name is passed as a number so it is
// NULL, which means "any title".
function FindWindowW(ClassName: String; WindowName: Cardinal): Longint;
  external 'FindWindowW@user32.dll stdcall';

function CredDeleteW(TargetName: String; CredType: Cardinal; Flags: Cardinal): Integer;
  external 'CredDeleteW@advapi32.dll stdcall delayload';

// `Yozora.exe stop` posts the tray's own Quit and waits for Yozora to exit,
// so the tray icon is removed rather than left as a ghost, and only processes
// of this install are touched. Builds older
// than the stop command answer "unknown command" with a non-zero code; for
// them, and for anything still running, taskkill without /F asks windows to
// close, and /F follows only after a short wait.
procedure StopYozora(Exe: String);
var
  Code: Integer;
begin
  if FileExists(Exe) and Exec(Exe, 'stop', '', SW_HIDE, ewWaitUntilTerminated, Code) and (Code = 0) then
    Exit;
  if Exec(ExpandConstant('{sys}\taskkill.exe'), '/IM Yozora.exe', '', SW_HIDE, ewWaitUntilTerminated, Code) and (Code = 0) then
    Sleep(2000);
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM Yozora.exe', '', SW_HIDE, ewWaitUntilTerminated, Code);
end;

function YozoraRunning: Boolean;
begin
  Result := FindWindowW('YozoraTrayHost', 0) <> 0;
end;

// Looked at before anything else can close Yozora (Windows Restart Manager
// closes applications that hold files, which happens around PrepareToInstall).
function InitializeSetup(): Boolean;
begin
  RunningAtStart := YozoraRunning;
  Result := True;
end;

// Upgrades replace Yozora.exe in place, so stop the running copy first. Whether it
// was running is recorded first; a fresh install has no Yozora.exe yet and never
// restarts anything. Setup started by Yozora's own Update now finds it running here.
function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  if FileExists(ExpandConstant('{app}\Yozora.exe')) then
  begin
    WasRunning := RunningAtStart or YozoraRunning;
    StopYozora(ExpandConstant('{app}\Yozora.exe'));
  end;
  Result := '';
end;

function RestartAfterUpgrade: Boolean;
begin
  Result := WasRunning;
end;

procedure DeleteDirWithRetry(Dir: String);
var
  I: Integer;
begin
  // WebView2 helper processes can hold the profile for a moment after exit.
  for I := 1 to 5 do
  begin
    if not DirExists(Dir) then
      Exit;
    DelTree(Dir, True, True, True);
    if not DirExists(Dir) then
      Exit;
    Sleep(500);
  end;
end;

// Paths match internal/config Paths(), internal/credman and the doctor probe;
// they keep the uika-resonance name for compatibility.
procedure DeleteUserData();
begin
  DeleteDirWithRetry(ExpandConstant('{userappdata}\uika-resonance'));
  DeleteDirWithRetry(ExpandConstant('{localappdata}\uika-resonance'));
  DeleteDirWithRetry(AddBackslash(GetTempDir) + 'uika-probe');
  CredDeleteW('uika-resonance:spotify-refresh', CRED_TYPE_GENERIC, 0);
  CredDeleteW('uika-resonance:spotify-secret', CRED_TYPE_GENERIC, 0);
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then
  begin
    // Opt-in only. A silent uninstall keeps data unless run with /PURGE=1.
    if UninstallSilent then
      RemoveUserData := ExpandConstant('{param:PURGE|0}') = '1'
    else
      RemoveUserData := MsgBox('Also remove my settings and cached data?' + #13#10 + #13#10 +
        'This deletes your Yozora settings, artwork cache, logs, the control panel''s browser data ' +
        'and any Spotify sign-in saved in Windows Credential Manager. Choose No to keep them for a later reinstall.',
        mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES;
    StopYozora(ExpandConstant('{app}\Yozora.exe'));
  end;
  if (CurUninstallStep = usPostUninstall) and RemoveUserData then
    DeleteUserData();
end;
