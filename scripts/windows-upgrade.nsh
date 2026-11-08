; Keep electron-builder's UUIDv5 uninstall identity for com.image2ai.desktop.
; This hook runs only when the user runs the installer. It never touches AppData.
!define LEGACY_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\1e81dbe1-01eb-519d-8dd4-7af4a4a5ce77"
!define NATIVE_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\com.image2ai.desktop"

!macro customInit
  SetRegView 64
  ReadRegStr $R0 HKCU "${LEGACY_KEY}" "InstallLocation"
  StrCmp $R0 "" 0 image2ai_found_install
  SetRegView 32
  ReadRegStr $R0 HKCU "${LEGACY_KEY}" "InstallLocation"
  SetRegView 64
  StrCmp $R0 "" image2ai_init_done
  image2ai_found_install:
  StrCpy $INSTDIR $R0
  image2ai_init_done:
!macroend

!macro customInstall
  ; MyGo has already committed the native executable and uninstaller.
  ; Copy its current metadata to the established uninstall key.
  ReadRegStr $R0 HKCU "${NATIVE_KEY}" "DisplayVersion"
  WriteRegStr HKCU "${LEGACY_KEY}" "DisplayName" "Image2AI"
  WriteRegStr HKCU "${LEGACY_KEY}" "DisplayVersion" $R0
  WriteRegStr HKCU "${LEGACY_KEY}" "DisplayIcon" '$\"$INSTDIR\Image2AI.exe$\"'
  WriteRegStr HKCU "${LEGACY_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${LEGACY_KEY}" "UninstallString" '$\"$INSTDIR\Uninstall.exe$\"'
  WriteRegStr HKCU "${LEGACY_KEY}" "QuietUninstallString" '$\"$INSTDIR\Uninstall.exe$\" /S'
  WriteRegDWORD HKCU "${LEGACY_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${LEGACY_KEY}" "NoRepair" 1
  DeleteRegKey HKCU "${NATIVE_KEY}"
  SetRegView 32
  DeleteRegKey HKCU "${LEGACY_KEY}"
  DeleteRegKey HKCU "${NATIVE_KEY}"
  SetRegView 64
  ; Remove only known obsolete Electron files, never arbitrary directories.
  Delete "$INSTDIR\Uninstall Image2AI.exe"
  Delete "$INSTDIR\resources\app.asar"
  Delete "$INSTDIR\chrome_100_percent.pak"
  Delete "$INSTDIR\chrome_200_percent.pak"
  Delete "$INSTDIR\resources.pak"
  Delete "$INSTDIR\icudtl.dat"
  Delete "$INSTDIR\snapshot_blob.bin"
  Delete "$INSTDIR\v8_context_snapshot.bin"
  Delete "$INSTDIR\d3dcompiler_47.dll"
  Delete "$INSTDIR\ffmpeg.dll"
  Delete "$INSTDIR\libEGL.dll"
  Delete "$INSTDIR\libGLESv2.dll"
  Delete "$INSTDIR\vk_swiftshader.dll"
  Delete "$INSTDIR\vk_swiftshader_icd.json"
  Delete "$INSTDIR\vulkan-1.dll"
!macroend

!macro customUnInstall
  SetRegView 64
  DeleteRegKey HKCU "${LEGACY_KEY}"
  SetRegView 32
  DeleteRegKey HKCU "${LEGACY_KEY}"
  SetRegView 64
!macroend
