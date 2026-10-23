; omniHub Windows 安装包（Inno Setup 6）。由发版工作流调用：
;   iscc /DAppVersion=0.1.0 /DSourceDir=<已部署的程序目录> /DOutputDir=<输出目录> packaging\windows\omnihub.iss
; 装到当前用户目录，不需要管理员权限；开始菜单与桌面快捷方式启动服务并打开桌面客户端。
#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

[Setup]
AppId={{6B1D2C1E-6F0E-4C47-9C1B-2C5F2B8A0D11}
AppName=omniHub
AppVersion={#AppVersion}
AppPublisher=cxy-251
AppPublisherURL=https://github.com/cxy-251/omniHub
DefaultDirName={localappdata}\Programs\omniHub
DefaultGroupName=omniHub
PrivilegesRequired=lowest
OutputDir={#OutputDir}
OutputBaseFilename=omniHub-{#AppVersion}-windows-x64-setup
Compression=lzma2
SolidCompression=yes
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
SetupIconFile={#SourceDir}\omnihub.ico
UninstallDisplayIcon={app}\omnihub.ico
WizardStyle=modern

[Languages]
Name: "default"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "创建桌面快捷方式"; GroupDescription: "快捷方式："

[Files]
Source: "{#SourceDir}\*"; DestDir: "{app}"; Flags: recursesubdirs ignoreversion

[Icons]
Name: "{group}\omniHub"; Filename: "{app}\omnihub-server.exe"; Parameters: "--desktop"; IconFilename: "{app}\omnihub.ico"
Name: "{group}\卸载 omniHub"; Filename: "{uninstallexe}"
Name: "{userdesktop}\omniHub"; Filename: "{app}\omnihub-server.exe"; Parameters: "--desktop"; IconFilename: "{app}\omnihub.ico"; Tasks: desktopicon

[Run]
Filename: "{app}\omnihub-server.exe"; Parameters: "--desktop"; Description: "启动 omniHub"; Flags: nowait postinstall skipifsilent
