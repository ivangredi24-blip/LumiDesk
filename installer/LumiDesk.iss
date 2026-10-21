#define RootDir ".."
#define ProductName "LumiDesk"
#define ProductVersion "1.0.0"

[Setup]
AppId={{BFFEE920-A349-4691-9B42-A0FC0D68E153}
AppName={#ProductName}
AppVersion={#ProductVersion}
AppPublisher=LumiDesk contributors
AppCopyright=Copyright © 2026 LumiDesk contributors
DefaultDirName={localappdata}\Programs\LumiDesk
DefaultGroupName=LumiDesk
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir={#RootDir}\build
OutputBaseFilename=LumiDesk-Setup-x64
SetupIconFile={#RootDir}\build\windows\icon.ico
UninstallDisplayIcon={app}\LumiDesk.exe
UninstallDisplayName=LumiDesk
VersionInfoVersion=1.0.0.0
VersionInfoProductVersion={#ProductVersion}
VersionInfoCompany=LumiDesk
VersionInfoDescription=LumiDesk Bluetooth RGB Lighting Controller Setup
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
CloseApplicationsFilter=LumiDesk.exe
RestartApplications=no
UsePreviousAppDir=yes
SetupLogging=yes

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Additional shortcuts:"; Flags: unchecked

[Files]
Source: "{#RootDir}\build\LumiDesk.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#RootDir}\installer\dependencies\MicrosoftEdgeWebView2Setup.exe"; DestDir: "{tmp}"; DestName: "LumiDeskWebView2Setup.exe"; Flags: deleteafterinstall
Source: "{#RootDir}\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#RootDir}\LICENSE-NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#RootDir}\third_party\licenses\*"; DestDir: "{app}\third_party\licenses"; Flags: recursesubdirs createallsubdirs ignoreversion

[Icons]
Name: "{group}\LumiDesk"; Filename: "{app}\LumiDesk.exe"; WorkingDir: "{app}"
Name: "{group}\Uninstall LumiDesk"; Filename: "{uninstallexe}"
Name: "{autodesktop}\LumiDesk"; Filename: "{app}\LumiDesk.exe"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
Filename: "{tmp}\LumiDeskWebView2Setup.exe"; Parameters: "/silent /install"; StatusMsg: "Checking the Microsoft Edge WebView2 Runtime..."; Flags: waituntilterminated; Check: not IsWebView2Installed
Filename: "{app}\LumiDesk.exe"; Description: "Launch LumiDesk"; WorkingDir: "{app}"; Flags: postinstall nowait skipifsilent

[Code]
const
  WebView2RuntimeClient = '{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}';

function RuntimeVersionPresent(RootKey: Integer; SubKey: String): Boolean;
var
  Version: String;
begin
  Result := RegQueryStringValue(RootKey, SubKey, 'pv', Version) and
    (Version <> '') and (Version <> '0.0.0.0');
end;

function IsWebView2Installed: Boolean;
var
  Key: String;
begin
  Key := 'Software\Microsoft\EdgeUpdate\Clients\' + WebView2RuntimeClient;
  Result := RuntimeVersionPresent(HKCU, Key) or RuntimeVersionPresent(HKLM, Key);
  if IsWin64 and not Result then
  begin
    Key := 'Software\WOW6432Node\Microsoft\EdgeUpdate\Clients\' + WebView2RuntimeClient;
    Result := RuntimeVersionPresent(HKLM, Key);
  end;
end;

function InitializeUninstall: Boolean;
begin
  { Startup is per-user. Remove only LumiDesk's entry; user settings/presets stay in AppData. }
  RegDeleteValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run', 'LumiDesk');
  Result := True;
end;
