#!/usr/bin/env bash
# Builds the local test image pglens-pgext:18: postgres:18.6-alpine with the
# hypopg and pg_hint_plan extensions. Sources are downloaded here, outside the
# build container, because the sandbox proxy re-signs github.com and the
# container does not trust that certificate. Certificate checks stay on.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
mkdir -p "$here/src"
curl -fsSL -o "$here/src/hypopg.tgz" https://github.com/HypoPG/hypopg/archive/refs/tags/1.4.5.tar.gz
curl -fsSL -o "$here/src/hint.tgz" https://github.com/ossc-db/pg_hint_plan/archive/refs/tags/REL18_1_8_1.tar.gz
args=()
for v in HTTP_PROXY HTTPS_PROXY http_proxy https_proxy NO_PROXY no_proxy; do
	args+=(--build-arg "$v=${!v:-}")
done
docker build --network host "${args[@]}" -t pglens-pgext:18 -f "$here/Dockerfile" "$here"
