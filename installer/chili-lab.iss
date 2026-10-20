; Installateur Windows de Chili Lab (Inno Setup 6), pour l'utilisateur courant,
; sans droits administrateur. Construit par build.ps1 -Installer.

#ifndef AppVersion
  #define AppVersion "0.1.0"
#endif

[Setup]
AppId={{6E2B7C1A-3C4F-4F1E-9B7A-C41C1C1AB001}
AppName=Chili Lab
AppVersion={#AppVersion}
AppVerName=Chili Lab {#AppVersion}
AppPublisher=getchili
AppPublisherURL=https://getchili.github.io
AppSupportURL=https://github.com/getchili/chili-lab
DefaultDirName={autopf}\Chili Lab
DefaultGroupName=Chili Lab
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
OutputDir=..\dist
OutputBaseFilename=ChiliLabSetup-{#AppVersion}
SetupIconFile=..\..\chili\editors\windows\chili.ico
UninstallDisplayIcon={app}\Chili Lab.exe
UninstallDisplayName=Chili Lab {#AppVersion}
WizardStyle=modern
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
CloseApplications=yes

[Languages]
Name: "french"; MessagesFile: "compiler:Languages\French.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"

[InstallDelete]
; Exemples retirés : ne pas les laisser traîner après une mise à jour.
Type: files; Name: "{app}\examples\sos.chl"
Type: files; Name: "{app}\examples\fib.chl"

[Files]
Source: "..\Chili Lab.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\..\chili\examples\*.chl"; DestDir: "{app}\examples"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Chili Lab"; Filename: "{app}\Chili Lab.exe"; Comment: "Programme tes ESP32 en Chili"
Name: "{autodesktop}\Chili Lab"; Filename: "{app}\Chili Lab.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\Chili Lab.exe"; Description: "{cm:LaunchProgram,Chili Lab}"; Flags: nowait postinstall skipifsilent
