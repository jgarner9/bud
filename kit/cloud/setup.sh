#!/bin/sh
# Setup script for a Claude Code cloud environment (claude.ai/code). Paste it
# into the environment's settings; it runs each time a container is built
# fresh. It installs the bud binary and runs the same sync a machine runs, so
# the kit's manifest is the only list of what a session gets. It never
# installs the editor config, and holds no secret.
#
# A failing setup script stops the session from starting, so nothing here is
# fatal: a failed download or sync is said on stderr and the session starts
# without the kit.
set -u

bud=$HOME/.local/bin/bud
if ! curl -fsSL https://raw.githubusercontent.com/jgarner9/bud/main/install.sh | sh -s -- --no-install; then
	echo "bud: the install failed; this session starts without the kit" >&2
fi
if [ ! -x "$bud" ]; then
	echo "bud: no binary at $bud; this session starts without the kit" >&2
elif ! "$bud" kit sync; then
	echo "bud: kit sync failed; run it again from the session" >&2
fi
exit 0
