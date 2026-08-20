#!/bin/bash
# Bootstrap a pre-baked dsh image: write settings, install per-env plugins, then
# exec the Web UI. dsh itself is already on PATH from the image build.
#
# dsh refuses --host 0.0.0.0, so it binds 127.0.0.1:3081 and a TCP proxy
# publishes 0.0.0.0:3080 for docker -p.
set -euo pipefail

export DSH_HOME="${DSH_HOME:-/data/dsh}"
export DSH_TELEMETRY_DISABLED="${DSH_TELEMETRY_DISABLED:-1}"

if [ -n "${DSH_API_KEY:-}" ] && [ -z "${DEEPSEEK_API_KEY:-}" ]; then
  export DEEPSEEK_API_KEY="$DSH_API_KEY"
fi

mkdir -p "$DSH_HOME" /workspace
cd /workspace

if [ -f /bootstrap/settings.yaml ]; then
  cp /bootstrap/settings.yaml "$DSH_HOME/settings.yaml"
fi

# First start (or a wiped profile) needs the web profile directory.
dsh --profile web --dump-default-config >/dev/null 2>&1 || true

PROFILE_DIR="$DSH_HOME/profiles/web"
PROFILE_WS="$PROFILE_DIR/pnpm-workspace.yaml"
if [ -f "$PROFILE_WS" ]; then
  python3 /usr/local/bin/dsh-allow-builds.py ensure "$PROFILE_WS"
fi

install_plugin() {
  local src="$1"
  local attempt=1
  local log rc
  log="$(mktemp)"
  while [ "$attempt" -le 3 ]; do
    echo "installing plugin: $src"
    set +e
    dsh plugin --profile web add "$src" >"$log" 2>&1
    rc=$?
    set -e
    cat "$log"
    if [ "$rc" -eq 0 ]; then
      rm -f "$log"
      return 0
    fi
    if python3 /usr/local/bin/dsh-allow-builds.py allow "$PROFILE_WS" "$log"; then
      echo "updated allowBuilds, retrying plugin add ($attempt)"
      attempt=$((attempt + 1))
      continue
    fi
    rm -f "$log"
    return "$rc"
  done
  rm -f "$log"
  return 1
}

if [ -f /bootstrap/plugins.txt ]; then
  while IFS= read -r src || [ -n "$src" ]; do
    src="${src#"${src%%[![:space:]]*}"}"
    src="${src%"${src##*[![:space:]]}"}"
    case "$src" in
      ""|\#*) continue ;;
    esac
    install_plugin "$src"
  done < /bootstrap/plugins.txt
fi

DSH_PORT="${DSH_WEB_PORT:-3081}"
PUBLISH_PORT="${DSH_PUBLISH_PORT:-3080}"
HOST="${DSH_WEB_HOST:-127.0.0.1}"

trusted_args=()
add_trusted() {
  local h="$1"
  [ -z "$h" ] && return
  trusted_args+=(--trusted-host "$h")
}
add_trusted "127.0.0.1"
add_trusted "localhost"
add_trusted "${DSH_TRUSTED_HOST:-}"

echo "starting dsh web on ${HOST}:${DSH_PORT}, proxy 0.0.0.0:${PUBLISH_PORT}"
dsh web --host "$HOST" --port "$DSH_PORT" --no-open "${trusted_args[@]}" &
dsh_pid=$!

ready=0
for _ in $(seq 1 60); do
  if ! kill -0 "$dsh_pid" 2>/dev/null; then
    wait "$dsh_pid"
    exit $?
  fi
  if python3 -c "import socket; s=socket.create_connection(('${HOST}', ${DSH_PORT}), 1); s.close()" 2>/dev/null; then
    ready=1
    break
  fi
  sleep 0.5
done
if [ "$ready" != 1 ]; then
  echo "dsh web did not listen on ${HOST}:${DSH_PORT}" >&2
  kill "$dsh_pid" 2>/dev/null || true
  wait "$dsh_pid" || true
  exit 1
fi

python3 /usr/local/bin/dsh-proxy.py "$PUBLISH_PORT" "$DSH_PORT" &
proxy_pid=$!

cleanup() {
  kill "$dsh_pid" "$proxy_pid" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

wait -n "$dsh_pid" "$proxy_pid"
exit $?
