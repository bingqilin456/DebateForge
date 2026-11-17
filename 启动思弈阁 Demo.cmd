@echo off
setlocal
title DebateForge Demo Launcher
cd /d "%~dp0"
if errorlevel 1 goto directory_error
where pwsh.exe >nul 2>nul
if errorlevel 1 goto missing_powershell
echo Starting local Fake Demo. First startup may take several minutes.
pwsh.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0infra\demo\quick-start.ps1"
set "result=%ERRORLEVEL%"
echo.
if "%result%"=="0" echo Demo is ready. Closing this window will NOT stop Docker containers.
if not "%result%"=="0" echo Startup failed. Exit code: %result%. Read the error above.
echo Press any key to close this window.
pause >nul
exit /b %result%

:missing_powershell
echo PowerShell 7.2 or newer was not found. Install PowerShell 7 and try again.
echo Press any key to close this window.
pause >nul
exit /b 1

:directory_error
echo Cannot open the project directory. Extract the project to a local folder and try again.
pause
exit /b 1
