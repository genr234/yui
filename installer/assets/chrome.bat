@echo off
setlocal

tasklist /FI "IMAGENAME eq controller.exe" | find /I "controller.exe" >NUL
if errorlevel 1 (
  start "" "%~dp0controller.exe"
)

start "" chrome.exe --proxy-server=localhost:7070 %*

endlocal

