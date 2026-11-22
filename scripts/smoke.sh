#!/bin/sh
# Host smoke against a running mcon. Requires curl and the admin port.
# Usage: scripts/smoke.sh http://127.0.0.1:8688
set -eu
ADMIN=${1:-http://127.0.0.1:8688}
curl --fail --silent "$ADMIN/healthz"
echo
curl --fail --silent "$ADMIN/readyz"
echo
curl --fail --silent "$ADMIN/version"
echo
