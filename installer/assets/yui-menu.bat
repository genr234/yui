@echo off
setlocal

if not exist "%~dp0controller.exe" (
  echo controller.exe not found
  pause
  goto :EOF
)

start "" "%~dp0controller.exe" --menu

endlocal
