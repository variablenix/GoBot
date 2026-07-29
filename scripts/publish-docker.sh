#!/usr/bin/env bash
set -euo pipefail

# Build and publish GoBot from the main branch.
# Usage:
#   DOCKERHUB_USERNAME=your-user ./scripts/publish-docker.sh
#   DOCKERHUB_USERNAME=your-user ./scripts/publish-docker.sh 1.0.0

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

if [[ "$(git branch --show-current)" != "main" ]]; then
  echo "This script must be run from the main branch." >&2
  exit 1
fi

if [[ -n "$(git status --porcelain)" ]]; then
  echo "The working tree must be clean before publishing." >&2
  exit 1
fi

: "${DOCKERHUB_USERNAME:?Set DOCKERHUB_USERNAME to your Docker Hub username}"

image="${GOBOT_IMAGE:-docker.io/${DOCKERHUB_USERNAME}/gobot}"
tag="${1:-latest}"

echo "Building ${image}:${tag} from $(git rev-parse --short HEAD)..."
docker build \
  --tag "${image}:${tag}" \
  --tag "${image}:latest" \
  .

echo "Pushing ${image}:${tag}..."
docker push "${image}:${tag}"

if [[ "$tag" != "latest" ]]; then
  echo "Pushing ${image}:latest..."
  docker push "${image}:latest"
fi

echo "Published ${image}:${tag}"
