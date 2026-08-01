#!/usr/bin/env bash

set -euo pipefail

service_name="gobot"
repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
install_dir="${GOBOT_INSTALL_DIR:-${repo_dir}}"
service_user="${GOBOT_SERVICE_USER:-${SUDO_USER:-$(id -un)}}"
service_group="${GOBOT_SERVICE_GROUP:-}"

usage() {
  cat <<'EOF'
Usage: sudo ./scripts/install-systemd.sh [options]

Install and enable the GoBot systemd unit without starting it.

Options:
  --install-dir DIR   GoBot deployment directory (default: this repository)
  --user USER         Linux account that runs GoBot (default: invoking user)
  --group GROUP       Linux group for GoBot (default: user's primary group)
  --help              Show this help

Environment overrides:
  GOBOT_INSTALL_DIR, GOBOT_SERVICE_USER, GOBOT_SERVICE_GROUP
EOF
}

while (($# > 0)); do
  case "$1" in
    --install-dir)
      [[ $# -ge 2 ]] || { echo "--install-dir requires a value" >&2; exit 2; }
      install_dir="$2"
      shift 2
      ;;
    --user)
      [[ $# -ge 2 ]] || { echo "--user requires a value" >&2; exit 2; }
      service_user="$2"
      shift 2
      ;;
    --group)
      [[ $# -ge 2 ]] || { echo "--group requires a value" >&2; exit 2; }
      service_group="$2"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run this installer with sudo so it can install the systemd unit." >&2
  exit 1
fi

command -v systemctl >/dev/null 2>&1 || {
  echo "systemctl was not found; this host may not use systemd." >&2
  exit 1
}

[[ -d "$install_dir" ]] || {
  echo "Deployment directory does not exist: $install_dir" >&2
  exit 1
}

template_path="$repo_dir/deploy/systemd/gobot.service"
[[ -f "$template_path" ]] || {
  echo "Service template not found: $template_path" >&2
  exit 1
}

id "$service_user" >/dev/null 2>&1 || {
  echo "Service user does not exist: $service_user" >&2
  exit 1
}

if [[ -z "$service_group" ]]; then
  service_group="$(id -gn "$service_user")"
fi

getent group "$service_group" >/dev/null 2>&1 || {
  echo "Service group does not exist: $service_group" >&2
  exit 1
}

if [[ ! -x "$install_dir/bin/irc-bot" ]]; then
  echo "Warning: binary not found or not executable: $install_dir/bin/irc-bot" >&2
  echo "Build it with ./scripts/build.sh before starting the service." >&2
fi

if [[ ! -f "$install_dir/config.yaml" ]]; then
  echo "Warning: config.yaml was not found in $install_dir" >&2
fi

if [[ ! -f "$install_dir/.env" ]]; then
  echo "Note: no .env file was found; the unit will still install, but secrets will be unavailable until you create it." >&2
fi

sed_escape() {
  printf '%s' "$1" | sed 's/[\\&|]/\\&/g'
}

escaped_dir="$(sed_escape "$install_dir")"
escaped_user="$(sed_escape "$service_user")"
escaped_group="$(sed_escape "$service_group")"
unit_path="/etc/systemd/system/${service_name}.service"
temporary_unit="$(mktemp)"
trap 'rm -f "$temporary_unit"' EXIT

sed \
  -e "s|@GOBOT_DIR@|$escaped_dir|g" \
  -e "s|@GOBOT_USER@|$escaped_user|g" \
  -e "s|@GOBOT_GROUP@|$escaped_group|g" \
  "$template_path" > "$temporary_unit"

install -o root -g root -m 0644 "$temporary_unit" "$unit_path"
systemctl daemon-reload
systemctl enable "${service_name}.service"

echo
echo "Installed: $unit_path"
echo "Enabled:   ${service_name}.service"
echo
echo "The service was not started. Review config.yaml and .env, build the binary, then run:"
echo "  sudo systemctl start ${service_name}.service"
echo "  sudo systemctl status ${service_name}.service"
