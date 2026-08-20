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

if [ "$PUSH" -eq 1 ]; then
  docker push "$tag"
  echo "pushed $tag"
else
  echo "built $tag (not pushed)"
fi
