#!/bin/sh
# Setup script for a Claude Code cloud environment (claude.ai/code). Paste it
# into the environment's settings; it runs each time a container is built
# fresh. It installs the bud binary and runs the same sync a machine runs, so
# the kit's manifest is the only list of what a session gets. It never
# installs the editor config, and holds no secret.
set -eu

curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh | sh -s -- --no-install
"$HOME/.local/bin/bud" kit sync
