$ErrorActionPreference = 'Stop'
python (Join-Path $PSScriptRoot 'scripts/start.py')
exit $LASTEXITCODE
