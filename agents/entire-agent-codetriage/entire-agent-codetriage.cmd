@echo off
setlocal
set "SCRIPT_DIR=%~dp0"
set "PYTHONPATH=%SCRIPT_DIR%src;%PYTHONPATH%"
python -m entire_agent_codetriage %*
exit /b %ERRORLEVEL%
