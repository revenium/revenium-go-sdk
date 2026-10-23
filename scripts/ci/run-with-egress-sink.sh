#!/usr/bin/env bash
# Runs a command with HTTP_PROXY and HTTPS_PROXY pointed at egress-sink.py
# and fails when any request reached the sink, that is, when a test tried to
# reach a host other than a local test server.
set -euo pipefail

if [[ $# -eq 0 ]]; then
  printf 'usage: %s <command> [args...]\n' "$0" >&2
  exit 2
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
work_dir="$(mktemp -d)"
log_path="${EGRESS_LOG:-${work_dir}/egress.log}"
port_path="${work_dir}/port"

python3 "${script_dir}/egress-sink.py" "$log_path" "$port_path" &
sink_pid=$!
disown "$sink_pid"
trap 'kill "$sink_pid" 2>/dev/null || true; rm -rf "$work_dir"' EXIT

for _ in $(seq 1 100); do
  [[ -s "$port_path" ]] && break
  sleep 0.1
done
if [[ ! -s "$port_path" ]]; then
  printf '::error::The egress sink did not start.\n' >&2
  exit 1
fi

proxy="http://127.0.0.1:$(cat "$port_path")"
HTTP_PROXY="$proxy" HTTPS_PROXY="$proxy" http_proxy="$proxy" https_proxy="$proxy" NO_PROXY= no_proxy= "$@" || status=$?

if [[ -s "$log_path" ]]; then
  printf '::error::Tests tried to reach hosts that are not local test servers:\n' >&2
  cat "$log_path" >&2
  exit 1
fi
printf 'Egress sink at %s recorded no outbound requests.\n' "$proxy"
exit "${status:-0}"
