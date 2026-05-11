@echo off
setlocal

cd /d "%~dp0"
set "DASHBOARD_DIR=%cd%"
set "GENERATOR=%cd%\claudescope-windows-amd64.exe"
set "DATA_PATH=%cd%\data.js"

if exist "%cd%\ClaudeScope Files\app\index.html" (
  set "DASHBOARD_DIR=%cd%\ClaudeScope Files\app"
  set "GENERATOR=%cd%\ClaudeScope Files\bin\claudescope-windows-amd64.exe"
  set "DATA_PATH=%cd%\ClaudeScope Files\app\data.js"
) else if exist "%cd%\app\index.html" (
  set "DASHBOARD_DIR=%cd%\app"
  set "GENERATOR=%cd%\bin\claudescope-windows-amd64.exe"
  set "DATA_PATH=%cd%\app\data.js"
) else (
  if not exist "%cd%\index.html" cd /d "%~dp0.."
)

if exist "%GENERATOR%" (
  "%GENERATOR%" generate --out "%DATA_PATH%"
  if errorlevel 1 goto generator_failed
  goto open_dashboard
)

where go >nul 2>nul
if %errorlevel%==0 (
  go build -trimpath -ldflags "-s -w" -o claudescope.exe ./generator/cmd/claudescope
  if errorlevel 1 goto generator_failed
  "%cd%\claudescope.exe" generate --out "%DATA_PATH%"
  if errorlevel 1 goto generator_failed
  goto open_dashboard
)

echo 未找到预构建的 claudescope，且本机未安装 Go。
echo 请从 GitHub Releases 下载 ClaudeScope-windows.zip。
pause
exit /b 1

:open_dashboard
start "" "%DASHBOARD_DIR%\index.html"
exit /b 0

:generator_failed
echo 生成 data.js 失败。
pause
exit /b 1
