#!/usr/bin/env bash
set -euo pipefail

# This script expects the official `act_runner` binary to already be installed
# and available on PATH. It never writes the registration token to this repo.

instance_url="${GITEA_INSTANCE_URL:-}"
runner_name="${GITEA_RUNNER_NAME:-gobot-ci}"
runner_labels="${GITEA_RUNNER_LABELS:-ubuntu-latest}"
runner_dir="${GITEA_RUNNER_DIR:-$PWD/.act-runner}"

runner_bin="${ACT_RUNNER_BIN:-}"
if [[ -z "$runner_bin" ]] && command -v act_runner >/dev/null 2>&1; then
  runner_bin="$(command -v act_runner)"
fi
for candidate in /opt/gitea-runner/act_runner /usr/local/bin/act_runner "$PWD/act_runner"; do
  if [[ -z "$runner_bin" && -x "$candidate" ]]; then
    runner_bin="$candidate"
  fi
done

if [[ -z "$runner_bin" ]]; then
  echo "act_runner was not found on PATH. Install it from the Gitea act runner releases first." >&2
  echo "See: https://docs.gitea.com/usage/actions/act-runner" >&2
  exit 1
fi

if [[ -z "$instance_url" ]]; then
  read -r -p "Gitea instance URL (for example https://gitea.example.com): " instance_url
fi

mkdir -p "$runner_dir"
cd "$runner_dir"

if [[ ! -f .runner ]]; then
  if [[ -n "${GITEA_RUNNER_REGISTRATION_TOKEN:-}" ]]; then
    registration_token="$GITEA_RUNNER_REGISTRATION_TOKEN"
  else
    read -r -s -p "Paste the runner registration token: " registration_token
    printf '\n'
  fi

  if [[ -z "$registration_token" ]]; then
    echo "A registration token is required." >&2
    exit 1
  fi

  "$runner_bin" register \
    --no-interactive \
    --instance "$instance_url" \
    --token "$registration_token" \
    --name "$runner_name" \
    --labels "$runner_labels"
else
  echo "Existing runner registration found in $runner_dir/.runner; skipping registration."
fi

exec "$runner_bin" daemon
