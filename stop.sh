#!/usr/bin/env bash
# Stops everything ./start.sh brought up. Data stays (`make down` keeps
# volumes) — running ./start.sh again picks up right where this left off.
set -euo pipefail
cd "$(dirname "$0")"

make down

echo "Scout stopped."
