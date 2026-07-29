#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_dir="$(cd "${script_dir}/.." && pwd)"
output_path="${repo_dir}/bin/irc-bot"

if ! command -v go >/dev/null 2>&1; then
  echo "Go is not installed or not on PATH." >&2
  echo "Install Go first, then rerun this script." >&2
  exit 1
fi

if ! command -v make >/dev/null 2>&1; then
  echo "make is not installed or not on PATH." >&2
  exit 1
fi

echo "Building GoBot..."
echo "Go version: $(go version)"

(
  cd "${repo_dir}"
  make build
)

echo "Build complete: ${output_path}"

if command -v file >/dev/null 2>&1; then
  file "${output_path}"
fi
