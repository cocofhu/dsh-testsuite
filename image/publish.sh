#!/usr/bin/env bash
# Build (and optionally push) one frozen dsh runtime tag.
# Usage: image/publish.sh --repo ghcr.io/owner/dsh-testsuite-runtime --version 0.1.0-rc.8 [--force] [--push]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IMAGE_DIR="$ROOT/image"
REPO=""
VERSION=""
FORCE=0
PUSH=0
GIT_SHA="${GITHUB_SHA:-}"
SOURCE="${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY:-cocofhu/dsh-testsuite}"

usage() {
  echo "usage: $0 --repo <name> --version <dshVersion> [--force] [--push]" >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --repo) REPO="${2:-}"; shift 2 ;;
    --version) VERSION="${2:-}"; shift 2 ;;
    --force) FORCE=1; shift ;;
    --push) PUSH=1; shift ;;
    -h|--help) usage ;;
    *) echo "unknown arg: $1" >&2; usage ;;
  esac
done

[ -n "$REPO" ] && [ -n "$VERSION" ] || usage

DOCKERFILE="$IMAGE_DIR/$VERSION/Dockerfile"
if [ ! -f "$DOCKERFILE" ]; then
  echo "missing $DOCKERFILE (add image/$VERSION/ before listing $VERSION in versions.txt)" >&2
  exit 1
fi

npm_ver="$(npm view "@deepseek-ai/dsh@${VERSION}" version 2>/dev/null || true)"
if [ "$npm_ver" != "$VERSION" ]; then
  echo "npm does not have @deepseek-ai/dsh@${VERSION} (got ${npm_ver:-empty})" >&2
  exit 1
fi

tag="$REPO:$VERSION"
if [ "$FORCE" -ne 1 ]; then
  if docker manifest inspect "$tag" >/dev/null 2>&1; then
    echo "skip frozen $tag"
    exit 0
  fi
fi

if [ -z "$GIT_SHA" ] && command -v git >/dev/null && git -C "$ROOT" rev-parse HEAD >/dev/null 2>&1; then
  GIT_SHA="$(git -C "$ROOT" rev-parse HEAD)"
fi
[ -n "$GIT_SHA" ] || GIT_SHA="unknown"

echo "building $tag from $DOCKERFILE"
docker build \
  --build-arg "DSH_VERSION=$VERSION" \
  --label dsh-testsuite.runtime=1 \
  --label "dsh-testsuite.dsh-version=$VERSION" \
  --label "org.opencontainers.image.revision=$GIT_SHA" \
  --label "org.opencontainers.image.source=$SOURCE" \
  -f "$DOCKERFILE" \
  -t "$tag" \
  "$IMAGE_DIR"

got="$(docker run --rm --entrypoint dsh "$tag" --version)"
if [ "$got" != "$VERSION" ]; then
  echo "baked dsh version $got, want $VERSION" >&2
  exit 1
fi

# `dsh --version` only proves the package baked in; it cannot catch an
# entrypoint that passes a flag this version's `dsh web` rejects. Boot the image
# once and require the entrypoint to reach its "backgrounded" hold state.
# Set SKIP_RUNTIME_SMOKE=1 to skip.
if [ "${SKIP_RUNTIME_SMOKE:-0}" -ne 1 ]; then
  smoke="dsh-runtime-smoke-$$-$(date +%s)"
  docker rm -f "$smoke" >/dev/null 2>&1 || true
  docker run -d --name "$smoke" "$tag" >/dev/null
  smoke_ok=0
  for _ in $(seq 1 90); do
    if docker logs "$smoke" 2>&1 | grep -q "dsh web is backgrounded"; then
      smoke_ok=1
      break
    fi
    if [ "$(docker inspect -f '{{.State.Running}}' "$smoke" 2>/dev/null || echo false)" != "true" ]; then
      break
    fi
    sleep 1
  done
  smoke_logs="$(docker logs "$smoke" 2>&1 || true)"
  docker rm -f "$smoke" >/dev/null 2>&1 || true
  if [ "$smoke_ok" -ne 1 ]; then
    echo "runtime smoke test failed for $tag; entrypoint did not start dsh web:" >&2
    echo "$smoke_logs" >&2
    exit 1
  fi
  echo "runtime smoke test ok for $tag"
fi

if [ "$PUSH" -eq 1 ]; then
  docker push "$tag"
  echo "pushed $tag"
else
  echo "built $tag (not pushed)"
fi
