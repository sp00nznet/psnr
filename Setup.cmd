@echo off
rem psnr quick start: checks for Go (asks before installing it), builds the
rem server exactly as README "Step by step" does, and leaves a launcher here.
setlocal
cd /d "%~dp0"
set "LOG=%~dp0setup.log"
echo psnr setup %DATE% %TIME% > "%LOG%"

where go >nul 2>&1
if not errorlevel 1 goto build
echo Go is needed to build the server: about 70 MB, installed with winget.
choice /m "Install Go now"
if errorlevel 2 (
  echo Setup stopped. Install Go from https://go.dev/dl/ and run Setup.cmd again.
  goto end
)
winget install -e --id GoLang.Go --accept-source-agreements --accept-package-agreements >> "%LOG%" 2>&1
set "PATH=%PATH%;C:\Program Files\Go\bin"
where go >nul 2>&1
if errorlevel 1 (
  echo Go did not install. Details are in %LOG%
  goto end
)

:build
echo Building the server...
pushd server
go build -o psnr.exe . >> "%LOG%" 2>&1
if errorlevel 1 (
  popd
  echo The build failed. Details are in %LOG%
  goto end
)
popd

> "Start psnr.cmd" echo @echo off
>> "Start psnr.cmd" echo cd /d "%%~dp0server"
>> "Start psnr.cmd" echo start "" http://127.0.0.1:36101/
>> "Start psnr.cmd" echo psnr.exe
echo Built.

echo.
echo Other players connect to this server on TCP port 36100. Windows Firewall
echo blocks that unless a rule allows it (not needed if only this PC plays).
choice /m "Add a firewall rule for psnr now (asks for admin)"
if errorlevel 2 goto done
powershell -NoProfile -Command "Start-Process -Verb RunAs -Wait cmd -ArgumentList '/c netsh advfirewall firewall delete rule name=psnr & netsh advfirewall firewall add rule name=psnr dir=in action=allow protocol=TCP localport=36100 program=\"%~dp0server\psnr.exe\"'" >> "%LOG%" 2>&1
netsh advfirewall firewall show rule name=psnr >nul 2>&1
if errorlevel 1 (
  echo The rule was not added. docs\running.md shows the command to run as admin.
) else (
  echo Firewall rule "psnr" added: TCP 36100 for server\psnr.exe.
)

:done
echo Done. Run "Start psnr.cmd" to start the server.

:end
pause
