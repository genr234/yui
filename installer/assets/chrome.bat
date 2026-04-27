@echo off
REM YUI_KIOSK_BOOTSTRAP
setlocal

echo [%date% %time%] launching "%~dp0controller.exe" >> "%~dp0controller-bootstrap.log"
if not exist "%~dp0controller.exe" (
  echo [%date% %time%] controller.exe not found >> "%~dp0controller-bootstrap.log"
  goto :EOF
)
start "" "%~dp0controller.exe" %*

endlocal
