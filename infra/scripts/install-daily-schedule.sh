#!/usr/bin/env bash
# Installs a launchd agent that runs infra/scripts/daily-live-run.sh once a
# day at SCOUT_DAILY_RUN_HOUR:SCOUT_DAILY_RUN_MINUTE local time — macOS's own
# scheduler, not a new dependency. If the laptop is asleep or closed at that
# time, launchd runs the missed job as soon as the Mac wakes (standard
# launchd calendar-interval behavior), which matches ADR-018's own "the user
# finds out whenever they next open the laptop" trade-off rather than fighting
# it. Requires Docker Desktop to already be running at that point — this
# script does not start Docker Desktop itself; see its own comment in
# daily-live-run.sh for what happens if it isn't.
#
# Usage: infra/scripts/install-daily-schedule.sh [hour] [minute] [duration_seconds]
#   defaults: 9 0 7200  (09:00 local, 2-hour window)
set -euo pipefail
cd "$(dirname "$0")/../.."
REPO_ROOT="$(pwd)"

HOUR="${1:-9}"
MINUTE="${2:-0}"
DURATION_SECONDS="${3:-7200}"

LABEL="com.scout.daily-live-run"
PLIST_DIR="$HOME/Library/LaunchAgents"
PLIST_PATH="$PLIST_DIR/$LABEL.plist"
LOG_DIR="$HOME/Library/Logs"

mkdir -p "$PLIST_DIR" "$LOG_DIR"

cat >"$PLIST_PATH" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>$LABEL</string>
	<key>ProgramArguments</key>
	<array>
		<string>/bin/bash</string>
		<string>$REPO_ROOT/infra/scripts/daily-live-run.sh</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>SCOUT_DAILY_RUN_DURATION_SECONDS</key>
		<string>$DURATION_SECONDS</string>
		<key>PATH</key>
		<string>/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
	</dict>
	<key>StartCalendarInterval</key>
	<dict>
		<key>Hour</key>
		<integer>$HOUR</integer>
		<key>Minute</key>
		<integer>$MINUTE</integer>
	</dict>
	<key>StandardOutPath</key>
	<string>$LOG_DIR/scout-daily-run.log</string>
	<key>StandardErrorPath</key>
	<string>$LOG_DIR/scout-daily-run.log</string>
</dict>
</plist>
EOF

launchctl unload "$PLIST_PATH" >/dev/null 2>&1 || true
launchctl load "$PLIST_PATH"

echo "installed: $LABEL runs daily at $(printf '%02d:%02d' "$HOUR" "$MINUTE") for ${DURATION_SECONDS}s"
echo "logs:      $LOG_DIR/scout-daily-run.log"
echo "uninstall: infra/scripts/uninstall-daily-schedule.sh"
