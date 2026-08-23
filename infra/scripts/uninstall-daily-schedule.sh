#!/usr/bin/env bash
# Removes the launchd agent infra/scripts/install-daily-schedule.sh installs.
# Does not touch a live run already in progress — stop that yourself
# (`make down`) if you want it to stop immediately too.
set -euo pipefail

LABEL="com.scout.daily-live-run"
PLIST_PATH="$HOME/Library/LaunchAgents/$LABEL.plist"

if [ ! -f "$PLIST_PATH" ]; then
	echo "not installed: $PLIST_PATH does not exist"
	exit 0
fi

launchctl unload "$PLIST_PATH" >/dev/null 2>&1 || true
rm -f "$PLIST_PATH"
echo "uninstalled: $LABEL"
