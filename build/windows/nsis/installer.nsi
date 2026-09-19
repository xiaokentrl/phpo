; phpo Windows 安装包（NSIS / MUI2）
; 由 `wails3 package -platform windows` 调用 makensis 时注入 -D 宏；
; 亦可 standalone 编译：所有宏均有 !ifndef 默认值兜底。
Unicode true
!include "MUI2.nsh"
!include "x64.nsh"

; ---- 可由构建系统覆盖的参数（缺省值保证脚本独立可编译）----
!ifndef APP_NAME
  !define APP_NAME "phpo"
!endif
!ifndef APP_VERSION
  !define APP_VERSION "0.1.0"
!endif
!ifndef BINARY_NAME
  !define BINARY_NAME "phpo"
!endif
!ifndef EXE_NAME
  !define EXE_NAME "${APP_NAME}-setup-x64.exe"
!endif
!ifndef PUBLISHER
  !define PUBLISHER "phpo contributors"
!endif
!ifndef WEBSITE
  !define WEBSITE "https://github.com/xiaokentrl/phpo"
!endif
!ifndef BUILD_DIR
  !define BUILD_DIR "build/bin"
!endif
!ifndef ICON
  !define ICON "build/windows/icon.ico"
!endif
!ifndef LICENSE
  ; 无许可文件时不阻断打包
  !define LICENSE ""
!endif

Name "${APP_NAME}"
OutFile "${EXE_NAME}"
InstallDir "$PROGRAMFILES64\${APP_NAME}"
InstallDirRegKey HKLM "Software\${APP_NAME}" "InstallLocation"
RequestExecutionLevel admin   ; 写入 Program Files / 卸载注册表需管理员
SetCompressor /SOLID lzma
VIProductVersion "${APP_VERSION}.0"
VIAddVersionKey /LANG=2052 "ProductName" "${APP_NAME}"
VIAddVersionKey /LANG=2052 "ProductVersion" "${APP_VERSION}"
VIAddVersionKey /LANG=2052 "CompanyName" "${PUBLISHER}"
VIAddVersionKey /LANG=2052 "LegalCopyright" "Copyright © 2026 ${PUBLISHER}"
VIAddVersionKey /LANG=2052 "FileDescription" "面向 PHP 开发者的本地 Docker 化开发环境管理器"
VIAddVersionKey /LANG=2052 "FileVersion" "${APP_VERSION}"

!ifmacrodef MUI_ICON
!endif
!define MUI_ICON "${ICON}"
!define MUI_UNICON "${ICON}"
!define MUI_ABORTWARNING

!insertmacro MUI_PAGE_WELCOME
!ifdef LICENSE
  !insertmacro MUI_PAGE_LICENSE "${LICENSE}"
!endif
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\${BINARY_NAME}.exe"
!define MUI_FINISHPAGE_RUN_TEXT "启动 ${APP_NAME}"
!define MUI_FINISHPAGE_RUN_CHECKED
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "English"

Section "Install" SecInstall
  SectionIn RO
  SetOutPath "$INSTDIR"
  File "${BUILD_DIR}\${BINARY_NAME}.exe"

  ; 开始菜单快捷方式
  CreateDirectory "$SMPROGRAMS\${APP_NAME}"
  CreateShortCut "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk" "$INSTDIR\${BINARY_NAME}.exe"
  CreateShortCut "$SMPROGRAMS\${APP_NAME}\卸载 ${APP_NAME}.lnk" "$INSTDIR\uninstall.exe"
  ; 桌面快捷方式（可选）
  CreateShortCut "$DESKTOP\${APP_NAME}.lnk" "$INSTDIR\${BINARY_NAME}.exe"

  ; 注册表：添加/删除程序 + 安装路径
  WriteRegStr HKLM "Software\${APP_NAME}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}" "DisplayName" "${APP_NAME}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}" "DisplayVersion" "${APP_VERSION}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}" "Publisher" "${PUBLISHER}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}" "URLInfoAbout" "${WEBSITE}"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}" "DisplayIcon" "$INSTDIR\${BINARY_NAME}.exe"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}" "UninstallString" '"$INSTDIR\uninstall.exe"'
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}" "NoModify" 1
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}" "EstimatedSize" "$0"

  WriteUninstaller "$INSTDIR\uninstall.exe"
SectionEnd

Section "Uninstall"
  ; 结束运行中的进程再删文件
  ExecWait 'taskkill /F /IM "${BINARY_NAME}.exe"'
  Delete "$INSTDIR\${BINARY_NAME}.exe"
  Delete "$INSTDIR\uninstall.exe"
  Delete "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk"
  Delete "$SMPROGRAMS\${APP_NAME}\卸载 ${APP_NAME}.lnk"
  Delete "$DESKTOP\${APP_NAME}.lnk"
  RMDir "$SMPROGRAMS\${APP_NAME}"
  RMDir "$INSTDIR"
  DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}"
  DeleteRegKey HKLM "Software\${APP_NAME}"
  ; 注意：用户数据目录 %APPDATA%\phpo（含 phpo.db / 配置 / 回收站）默认保留，符合 §5.13.7「卸载保留数据」
SectionEnd
