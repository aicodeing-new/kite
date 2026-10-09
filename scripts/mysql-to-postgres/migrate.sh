#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
umask 077
command -v python3 >/dev/null || { echo "Install: sudo apt-get install python3 python3-venv"; exit 1; }
python3 -m venv .venv || { echo "Install: sudo apt-get install python3-venv"; exit 1; }
.venv/bin/python -m pip install --disable-pip-version-check -r requirements.txt
exec .venv/bin/python migrate.py "$@"
