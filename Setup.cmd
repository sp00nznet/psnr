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
echo Done. Run "Start psnr.cmd" to start the server.

:end
pause
