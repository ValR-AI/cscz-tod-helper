@echo off
rem Builds the Windows and Linux programs into dist\ and prints their SHA-256.
rem The Go version is pinned so that everyone gets byte-identical programs; Go
rem downloads that exact version by itself if yours is different.
setlocal
cd /d "%~dp0"
if "%GOTOOLCHAIN%"=="" set GOTOOLCHAIN=go1.24.7
set CGO_ENABLED=0
set GOARCH=amd64
set GOAMD64=v1
set GOFLAGS=
if not exist dist mkdir dist
set GOOS=windows
go build -trimpath -buildvcs=false -ldflags="-s -w" -o dist\cscz-tod-helper.exe . || exit /b 1
set GOOS=linux
go build -trimpath -buildvcs=false -ldflags="-s -w" -o dist\cscz-tod-helper . || exit /b 1
set GOOS=
go version
certutil -hashfile dist\cscz-tod-helper.exe SHA256
certutil -hashfile dist\cscz-tod-helper SHA256
