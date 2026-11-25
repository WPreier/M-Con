#!/bin/sh
# Local end-to-end test with real SMTP delivery.
# Usage (from repo root or anywhere):
#   scripts/local-mail-test.sh              # start mcon, send one mail, stop
#   scripts/local-mail-test.sh --keep       # start, send, leave mcon running
#   scripts/local-mail-test.sh --daemon     # only start mcon (foreground)
#   scripts/local-mail-test.sh --send-only  # send against an already running local mcon
#
# Required once:
#   cp configs/local/mcon.yaml.example configs/local/mcon.yaml
#   cp configs/local/secrets.yaml.example configs/local/secrets.yaml
#   cp configs/local/mcon-ctl.yaml.example configs/local/mcon-ctl.yaml
#   # edit host/username/password, allowed_from, and matching secrets/ctl values
#
# Optional env:
#   MCON_TEST_FROM   envelope From (must be in clients.local.allowed_from)
#   MCON_TEST_TO     recipient mailbox that you can check
#   MCON_TEST_SUBJECT
set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$ROOT"

CFG="$ROOT/configs/local/mcon.yaml"
SEC="$ROOT/configs/local/secrets.yaml"
CTL="$ROOT/configs/local/mcon-ctl.yaml"
PIDFILE="$ROOT/var/local/mcon.pid"
LOGFILE="$ROOT/var/local/mcon.log"
MSGFILE="$ROOT/var/local/contact-real.json"

MODE=run
KEEP=0
for arg in "$@"; do
  case "$arg" in
    --keep) KEEP=1 ;;
    --daemon) MODE=daemon ;;
    --send-only) MODE=send ;;
    -h|--help)
      sed -n '2,16p' "$0"
      exit 0
      ;;
    *)
      echo "unknown option: $arg" >&2
      exit 2
      ;;
  esac
done

need_configs() {
  missing=0
  for f in "$CFG" "$SEC" "$CTL"; do
    if [ ! -f "$f" ]; then
      echo "missing $f" >&2
      missing=1
    fi
  done
  if [ "$missing" -ne 0 ]; then
    echo "Copy the examples first:" >&2
    echo "  cp configs/local/mcon.yaml.example configs/local/mcon.yaml" >&2
    echo "  cp configs/local/secrets.yaml.example configs/local/secrets.yaml" >&2
    echo "  cp configs/local/mcon-ctl.yaml.example configs/local/mcon-ctl.yaml" >&2
    echo "Then set SMTP host/username, secrets password, allowed_from, and ctl hmac/token." >&2
    exit 1
  fi
}

build_bins() {
  v=$(tr -d ' \r\n' < VERSION)
  echo "building mcon / mcon-ctl ($v)..."
  go build -ldflags "-X mcon/internal/version.Version=$v" -o "$ROOT/bin/mcon" ./cmd/mcon
  go build -ldflags "-X mcon/internal/version.Version=$v" -o "$ROOT/bin/mcon-ctl" ./cmd/mcon-ctl
}

prepare_dirs() {
  mkdir -p "$ROOT/var/local/archive" "$ROOT/var/local/tmp" "$ROOT/var/local/backups" "$ROOT/bin"
}

wait_ready() {
  i=0
  while [ "$i" -lt 50 ]; do
    if curl --fail --silent "http://127.0.0.1:8688/readyz" >/dev/null 2>&1; then
      return 0
    fi
    i=$((i + 1))
    sleep 0.1
  done
  echo "mcon did not become ready; last log lines:" >&2
  tail -n 40 "$LOGFILE" 2>/dev/null || true
  return 1
}

stop_mcon() {
  if [ -f "$PIDFILE" ]; then
    pid=$(cat "$PIDFILE")
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
    rm -f "$PIDFILE"
  fi
}

start_mcon() {
  stop_mcon
  echo "starting mcon (log: $LOGFILE)..."
  (
    cd "$ROOT"
    "$ROOT/bin/mcon" -config "$CFG" -secrets "$SEC"
  ) >"$LOGFILE" 2>&1 &
  echo $! >"$PIDFILE"
  wait_ready
  echo "mcon ready (pid $(cat "$PIDFILE"))"
}

write_message() {
  from=${MCON_TEST_FROM:-noreply@example.com}
  to=${MCON_TEST_TO:-}
  subject=${MCON_TEST_SUBJECT:-M-Con local real-mail test}
  if [ -z "$to" ]; then
    printf "Recipient address (MCON_TEST_TO): "
    read -r to
  fi
  if [ -z "$to" ]; then
    echo "MCON_TEST_TO is required" >&2
    exit 1
  fi
  # Escape for JSON strings (minimal).
  esc() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'; }
  cat >"$MSGFILE" <<EOF
{
  "from": "$(esc "$from")",
  "to": ["$(esc "$to")"],
  "subject": "$(esc "$subject")",
  "text": "M-Con local real-mail test at $(date -u +%Y-%m-%dT%H:%M:%SZ).",
  "html": "<p>M-Con local real-mail test.</p>",
  "tags": ["local-test"],
  "metadata": {"source": "local-mail-test"}
}
EOF
  echo "message -> $MSGFILE (from=$from to=$to)"
}

send_mail() {
  write_message
  echo "sending..."
  out=$("$ROOT/bin/mcon-ctl" -config "$CTL" send --file "$MSGFILE")
  echo "$out"
  id=$(printf '%s' "$out" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p' | head -n 1)
  if [ -z "$id" ]; then
    echo "could not parse message id from send response" >&2
    exit 1
  fi
  echo "waiting for delivery ($id)..."
  status=$("$ROOT/bin/mcon-ctl" -config "$CTL" status --id "$id" --wait --timeout 60s)
  echo "$status"
  case "$status" in
    *'"state":"sent"'*)
      echo "OK: mail marked sent — check the recipient inbox (and spam folder)."
      ;;
    *)
      echo "delivery did not reach state=sent; see $LOGFILE" >&2
      exit 1
      ;;
  esac
}

need_configs
prepare_dirs
build_bins

case "$MODE" in
  daemon)
    trap 'stop_mcon' INT TERM
    echo "foreground local mcon — Ctrl-C to stop"
    echo "  admin:  http://127.0.0.1:8688/test/"
    echo "  health: http://127.0.0.1:8688/healthz"
    cd "$ROOT"
    exec "$ROOT/bin/mcon" -config "$CFG" -secrets "$SEC"
    ;;
  send)
    send_mail
    ;;
  run)
    trap 'if [ "$KEEP" -eq 0 ]; then stop_mcon; fi' EXIT INT TERM
    start_mcon
    send_mail
    if [ "$KEEP" -eq 1 ]; then
      trap - EXIT
      echo "mcon left running (pid $(cat "$PIDFILE")); stop with: kill \$(cat var/local/mcon.pid)"
      echo "test UI: http://127.0.0.1:8688/test/"
    fi
    ;;
esac
