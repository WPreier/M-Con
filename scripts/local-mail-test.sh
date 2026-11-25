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

admin_url() {
  # Prefer mcon-ctl.yaml admin_url so ports stay in sync with the local config.
  awk -F': *' '/^admin_url:/ { gsub(/["\r]/,"",$2); print $2; exit }' "$CTL"
}

public_url() {
  awk -F': *' '/^public_url:/ { gsub(/["\r]/,"",$2); print $2; exit }' "$CTL"
}

check_scheme() {
  pub=$(public_url)
  case "$pub" in
    https://*)
      echo "warning: public_url is HTTPS ($pub)." >&2
      echo "Local default has tls.enabled: false — use http://127.0.0.1:18687 unless you intentionally target a TLS listener." >&2
      ;;
    http://*)
      # Probe: plaintext HTTP against an HTTPS port yields this Go/net body.
      code=$(curl -s -o /tmp/mcon-local-probe.out -w '%{http_code}' "$pub/v1/messages" 2>/dev/null || true)
      if [ -f /tmp/mcon-local-probe.out ] && grep -qi 'Client sent an HTTP request to an HTTPS server' /tmp/mcon-local-probe.out 2>/dev/null; then
        echo "error: $pub speaks HTTPS, but mcon-ctl uses http://." >&2
        echo "Likely the production service on :8687 is answering (TLS on)." >&2
        echo "Fix: use local ports 18687/18688 (see configs/local/*.example), or set public_url to https://… and insecure_skip_verify as needed." >&2
        rm -f /tmp/mcon-local-probe.out
        exit 1
      fi
      rm -f /tmp/mcon-local-probe.out
      _=$code
      ;;
  esac
}

wait_ready() {
  adm=$(admin_url)
  if [ -z "$adm" ]; then
    adm="http://127.0.0.1:18688"
  fi
  i=0
  while [ "$i" -lt 50 ]; do
    if curl --fail --silent "$adm/readyz" >/dev/null 2>&1; then
      return 0
    fi
    i=$((i + 1))
    sleep 0.1
  done
  echo "mcon did not become ready at $adm/readyz; last log lines:" >&2
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
    echo "  admin:  $(admin_url)/test/"
    echo "  health: $(admin_url)/healthz"
    cd "$ROOT"
    exec "$ROOT/bin/mcon" -config "$CFG" -secrets "$SEC"
    ;;
  send)
    check_scheme
    send_mail
    ;;
  run)
    trap 'if [ "$KEEP" -eq 0 ]; then stop_mcon; fi' EXIT INT TERM
    start_mcon
    check_scheme
    send_mail
    if [ "$KEEP" -eq 1 ]; then
      trap - EXIT
      echo "mcon left running (pid $(cat "$PIDFILE")); stop with: kill \$(cat var/local/mcon.pid)"
      echo "test UI: $(admin_url)/test/"
    fi
    ;;
esac
