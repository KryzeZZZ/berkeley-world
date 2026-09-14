@echo off
setlocal
cd /d "%~dp0"
go run -buildvcs=false ./cmd/pixel_server
pause
