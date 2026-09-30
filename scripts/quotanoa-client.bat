@echo off
rem QuotaNoa Client helper (Windows CMD). ASCII-only so it works under any
rem Windows code page. The sh helper (scripts/quotanoa-client.sh) is UTF-8.
rem
rem Usage: scripts\quotanoa-client.bat [command] [args]
rem
rem Run with no arguments for an interactive console (menu + prompt, q to quit).
rem
rem Commands:
rem   interactive      interactive console (aliases: i / menu)
rem   init [--force]   write a default config file (--force overwrites)
rem   check            validate the config and print a summary (no network)
rem   patch            back up, then fill missing keys + write config_version
rem   version          print client and protocol version
rem   run [args]       run in the foreground (Ctrl-C to stop)
rem   start [args]     run in the background with logging
rem   stop             stop the background process
rem   restart [args]   restart the background process
rem   status           show run state (and config summary)
rem   logs [lines]     show the background log (default 40 lines)
rem   help             show this help
rem
rem Environment variables:
rem   QUOTANOA_BIN     binary path (default <repo>\quotanoa-client.exe)
rem   QUOTANOA_CONFIG  config path (default <repo>\config.json)
rem   QUOTANOA_LOG     background log path (default <repo>\quotanoa-client.log)
rem   QUOTANOA_PID     pid file path (default <repo>\quotanoa-client.pid)

setlocal EnableExtensions EnableDelayedExpansion

set "SCRIPT_DIR=%~dp0"
for %%I in ("%SCRIPT_DIR%..") do set "ROOT_DIR=%%~fI"

if not defined QUOTANOA_BIN set "QUOTANOA_BIN=%ROOT_DIR%\quotanoa-client.exe"
if not defined QUOTANOA_CONFIG set "QUOTANOA_CONFIG=%ROOT_DIR%\config.json"
if not defined QUOTANOA_LOG set "QUOTANOA_LOG=%ROOT_DIR%\quotanoa-client.log"
if not defined QUOTANOA_PID set "QUOTANOA_PID=%ROOT_DIR%\quotanoa-client.pid"

set "CMD=%~1"
set "REST="
:parse_rest
shift
if "%~1"=="" goto :parsed_rest
set "REST=!REST! %1"
goto :parse_rest
:parsed_rest

if "%CMD%"=="" set "CMD=interactive"
goto :dispatch

rem ---------------------------------------------------------------------------
rem Subroutines
rem ---------------------------------------------------------------------------

:require_bin
if exist "%QUOTANOA_BIN%" goto :eof
echo [ERROR] binary not found: %QUOTANOA_BIN% 1>&2
echo         build it with: go build -o quotanoa-client.exe ./cmd/quotanoa-client 1>&2
echo         or set QUOTANOA_BIN. 1>&2
exit /b 1

:read_pid
set "PID="
if exist "%QUOTANOA_PID%" set /p PID=<"%QUOTANOA_PID%"
goto :eof

:is_running
call :read_pid
if not defined PID exit /b 1
for /f "tokens=2" %%P in ('tasklist /FI "PID eq %PID%" /NH 2^>nul') do (
    if "%%P"=="%PID%" exit /b 0
)
exit /b 1

rem ---------------------------------------------------------------------------
rem Dispatch
rem ---------------------------------------------------------------------------

:dispatch
if /i "%CMD%"=="init" goto :cmd_init
if /i "%CMD%"=="check" goto :cmd_check
if /i "%CMD%"=="patch" goto :cmd_patch
if /i "%CMD%"=="version" goto :cmd_version
if /i "%CMD%"=="run" goto :cmd_run
if /i "%CMD%"=="start" goto :cmd_start
if /i "%CMD%"=="stop" goto :cmd_stop
if /i "%CMD%"=="restart" goto :cmd_restart
if /i "%CMD%"=="status" goto :cmd_status
if /i "%CMD%"=="logs" goto :cmd_logs
if /i "%CMD%"=="interactive" goto :cmd_interactive
if /i "%CMD%"=="i" goto :cmd_interactive
if /i "%CMD%"=="menu" goto :cmd_interactive
if /i "%CMD%"=="help" goto :cmd_help
if /i "%CMD%"=="-h" goto :cmd_help
if /i "%CMD%"=="--help" goto :cmd_help
echo [ERROR] unknown command: %CMD% (run help) 1>&2
exit /b 1

:cmd_init
call :require_bin
if errorlevel 1 exit /b 1
set "FORCE="
for %%A in (%REST%) do (
    if /i "%%~A"=="--force" set "FORCE=--force"
    if /i "%%~A"=="-f" set "FORCE=--force"
)
"%QUOTANOA_BIN%" config init --out "%QUOTANOA_CONFIG%" %FORCE%
exit /b %errorlevel%

:cmd_check
call :require_bin
if errorlevel 1 exit /b 1
"%QUOTANOA_BIN%" config check --config "%QUOTANOA_CONFIG%"
exit /b %errorlevel%

:cmd_patch
call :require_bin
if errorlevel 1 exit /b 1
"%QUOTANOA_BIN%" config patch --config "%QUOTANOA_CONFIG%"
exit /b %errorlevel%

:cmd_version
call :require_bin
if errorlevel 1 exit /b 1
"%QUOTANOA_BIN%" version
exit /b %errorlevel%

:cmd_run
call :require_bin
if errorlevel 1 exit /b 1
"%QUOTANOA_BIN%" run --config "%QUOTANOA_CONFIG%" %REST%
exit /b %errorlevel%

:cmd_start
call :require_bin
if errorlevel 1 exit /b 1
call :is_running
if not errorlevel 1 (
    call :read_pid
    echo [INFO] already running ^(PID !PID!^), not starting again.
    exit /b 0
)
set "QUOTANOA_EXTRA=%REST%"
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
    "$ErrorActionPreference='Stop';" ^
    "$out = $env:QUOTANOA_LOG; $err = $env:QUOTANOA_LOG + '.err';" ^
    "$extra = @(); if ($env:QUOTANOA_EXTRA -and $env:QUOTANOA_EXTRA.Trim()) { $extra = $env:QUOTANOA_EXTRA.Trim() -split '\s+' };" ^
    "$p = Start-Process -FilePath $env:QUOTANOA_BIN -ArgumentList (@('run','--config',$env:QUOTANOA_CONFIG) + $extra) -RedirectStandardOutput $out -RedirectStandardError $err -PassThru -WindowStyle Hidden;" ^
    "Start-Sleep -Milliseconds 500;" ^
    "if ($p.HasExited) { exit 1 };" ^
    "$p.Id | Set-Content -Encoding ascii -Path $env:QUOTANOA_PID"
if errorlevel 1 (
    echo [ERROR] failed to start; see log: %QUOTANOA_LOG% 1>&2
    exit /b 1
)
call :read_pid
echo [OK] started ^(PID !PID!^). log: %QUOTANOA_LOG%
exit /b 0

:cmd_stop
call :is_running
if errorlevel 1 (
    del "%QUOTANOA_PID%" >nul 2>&1
    echo [INFO] not running.
    exit /b 0
)
call :read_pid
taskkill /PID !PID! /T /F >nul 2>&1
del "%QUOTANOA_PID%" >nul 2>&1
echo [OK] stopped ^(PID !PID!^).
exit /b 0

:cmd_restart
call :cmd_stop
call :cmd_start
exit /b %errorlevel%

:cmd_status
call :is_running
if errorlevel 1 goto :status_stopped
call :read_pid
echo [STATE] running ^(PID !PID!^)
goto :status_env
:status_stopped
echo [STATE] not running.
:status_env
echo binary: %QUOTANOA_BIN%
echo config: %QUOTANOA_CONFIG%
if exist "%QUOTANOA_BIN%" if exist "%QUOTANOA_CONFIG%" "%QUOTANOA_BIN%" config check --config "%QUOTANOA_CONFIG%"
exit /b 0

:cmd_logs
if not exist "%QUOTANOA_LOG%" (
    echo [ERROR] log not found: %QUOTANOA_LOG% 1>&2
    exit /b 1
)
set "N=40"
if not "%REST%"=="" set "N=%REST%"
powershell -NoProfile -Command "Get-Content -Tail !N! -Path $env:QUOTANOA_LOG"
exit /b %errorlevel%

:dispatch_line
rem Execute one interactive line (number or "command [args]").
rem Returns errorlevel 1 to quit the interactive console.
rem Tokenize first: for /f strips the stray trailing whitespace that
rem `set /p` may read from a pipe, then map numeric shortcuts.
set "L=%~1"
set "CMD="
set "REST="
for /f "tokens=1,*" %%A in ("!L!") do (
    set "CMD=%%A"
    set "REST=%%B"
)
if not defined CMD exit /b 0
if /i "!CMD!"=="q" exit /b 1
if /i "!CMD!"=="quit" exit /b 1
if /i "!CMD!"=="exit" exit /b 1
if "!CMD!"=="1" set "CMD=init"
if "!CMD!"=="2" set "CMD=check"
if "!CMD!"=="3" set "CMD=patch"
if "!CMD!"=="4" set "CMD=start"
if "!CMD!"=="5" set "CMD=stop"
if "!CMD!"=="6" set "CMD=restart"
if "!CMD!"=="7" set "CMD=status"
if "!CMD!"=="8" set "CMD=logs"
if "!CMD!"=="9" set "CMD=version"
if /i "!CMD!"=="init" ( call :cmd_init & exit /b 0 )
if /i "!CMD!"=="check" ( call :cmd_check & exit /b 0 )
if /i "!CMD!"=="patch" ( call :cmd_patch & exit /b 0 )
if /i "!CMD!"=="version" ( call :cmd_version & exit /b 0 )
if /i "!CMD!"=="run" ( call :cmd_run & exit /b 0 )
if /i "!CMD!"=="start" ( call :cmd_start & exit /b 0 )
if /i "!CMD!"=="stop" ( call :cmd_stop & exit /b 0 )
if /i "!CMD!"=="restart" ( call :cmd_restart & exit /b 0 )
if /i "!CMD!"=="status" ( call :cmd_status & exit /b 0 )
if /i "!CMD!"=="logs" ( call :cmd_logs & exit /b 0 )
if /i "!CMD!"=="help" ( call :cmd_help & exit /b 0 )
echo [ERROR] unknown command: !CMD!
exit /b 0

:cmd_interactive
echo.
echo QuotaNoa Client interactive console ^(input a number or a command, q to quit^)
echo   1^) init      2^) check     3^) patch
echo   4^) start     5^) stop      6^) restart
echo   7^) status    8^) logs      9^) version
:interactive_loop
set "LINE="
set /p "LINE=quotanoa> "
if errorlevel 1 goto :interactive_exit
if not defined LINE goto :interactive_loop
call :dispatch_line "!LINE!"
if errorlevel 1 goto :interactive_exit
goto :interactive_loop
:interactive_exit
echo bye.
exit /b 0

:cmd_help
echo QuotaNoa Client helper (Windows)
echo.
echo Usage: scripts\quotanoa-client.bat ^<command^> [args]
echo.
echo   init [--force]   write a default config file (--force overwrites)
echo   check            validate the config and print a summary (no network)
echo   patch            back up, then fill missing keys + write config_version
echo   version          print client and protocol version
echo   run [args]       run in the foreground (Ctrl-C to stop)
echo   start [args]     run in the background with logging
echo   stop             stop the background process
echo   restart [args]   restart the background process
echo   status           show run state (and config summary)
echo   logs [lines]     show the background log (default 40 lines)
echo   help             show this help
echo.
echo Env: QUOTANOA_BIN / QUOTANOA_CONFIG / QUOTANOA_LOG / QUOTANOA_PID
exit /b 0
