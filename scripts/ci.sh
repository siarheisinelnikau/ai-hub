#!/bin/sh
# Portable entry point; install requirements-dev.txt before running.
set -eu
cd "$(dirname "$0")/.."
exec python scripts/check.py
