; Universal per-user installer. Build with scripts/Build-Installer.ps1 and Inno Setup 6.7.4.
; The 32-bit Setup bootstrap runs on Windows 10 ARM64 as well as x64. Payload selection is native.
#ifndef SourceRoot
  #define SourceRoot SourcePath + "\.."
#endif
#ifndef PayloadRoot
  #define PayloadRoot SourceRoot + "\out"
#endif
#ifndef BuildVersion
  #define BuildVersion "1.0.1"
#endif

[Setup]
AppId={{7AEBA055-83B4-4B99-A30B-92E5C9C0C5D0}
AppName=AI Reply
AppVersion={#BuildVersion}
AppPublisher=AI Reply
AppPublisherURL=https://ai-reply.kz/
AppSupportURL=https://ai-reply.kz/
AppUpdatesURL=https://ai-reply.kz/
VersionInfoVersion={#BuildVersion}
VersionInfoDescription=AI Reply Setup
VersionInfoCompany=AI Reply
VersionInfoProductName=AI Reply
DefaultDirName={localappdata}\Programs\AI Reply
DefaultGroupName=AI Reply
DisableDirPage=yes
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
UsePreviousAppDir=yes
UsePreviousTasks=yes
ArchitecturesAllowed=x64compatible or arm64
ArchitecturesInstallIn64BitMode=x64compatible or arm64
MinVersion=10.0
AppMutex=Local\AIReply-production
SetupMutex=Local\AIReply-Setup
CloseApplications=no
RestartApplications=no
Uninstallable=yes
UninstallDisplayName=AI Reply
UninstallDisplayIcon={app}\AIReply.exe
SetupIconFile={#SourceRoot}\src\AirReply.App\Assets\AIReply.ico
WizardSmallImageFile={#SourceRoot}\src\AirReply.App\Assets\AIReply-512.png
WizardImageFile={#SourceRoot}\installer\Assets\WizardLogo.png
OutputDir={#PayloadRoot}
OutputBaseFilename=AIReply-Setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
DisableWelcomePage=no
DisableReadyPage=yes
UninstallLogging=yes

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[Files]
Source: "{#PayloadRoot}\win-x64\AIReply.exe"; DestDir: "{app}"; Flags: ignoreversion; Check: not IsArm64
Source: "{#PayloadRoot}\win-arm64\AIReply.exe"; DestDir: "{app}"; Flags: ignoreversion; Check: IsArm64
Source: "{#SourceRoot}\README-Windows.txt"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\AI Reply"; Filename: "{app}\AIReply.exe"; WorkingDir: "{app}"; AppUserModelID: "kz.ai-reply.windows"
Name: "{autodesktop}\AI Reply"; Filename: "{app}\AIReply.exe"; WorkingDir: "{app}"; AppUserModelID: "kz.ai-reply.windows"; Tasks: desktopicon

[Run]
Filename: "{app}\AIReply.exe"; Description: "{cm:LaunchProgram,AI Reply}"; Flags: nowait postinstall skipifsilent

[Code]
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  ExistingCommand: String;
  InstalledCommand: String;
begin
  if CurUninstallStep <> usUninstall then
    Exit;

  { Preserve all account/session/profile files in LocalAppData\AI Reply.
    Remove autostart only if this installed copy owns it; a portable copy may own the same preference. }
  InstalledCommand := '"' + ExpandConstant('{app}\AIReply.exe') + '" --background';
  if RegQueryStringValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run', 'AI Reply', ExistingCommand) and
     (CompareText(Trim(ExistingCommand), InstalledCommand) = 0) then
  begin
    RegDeleteValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Run', 'AI Reply');
    RegDeleteValue(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run', 'AI Reply');
  end;
end;
