#!/usr/bin/env bash
set -euo pipefail
export AIHUB_MODEL_ID=qwen38-flash-next-uncensored-strata
export STRATA_PORT=18030
export STRATA_INSTALL_CONFIG=strata-dyluhn-uncensored-ud-iq4_xs.json
exec /srv/ai/strata/.venv/bin/python /srv/ai-hub/strata/launch.py
