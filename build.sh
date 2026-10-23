#!/bin/sh
# Builds the Windows and Linux programs into dist/ and writes their SHA-256.
# The Go version is pinned so that everyone gets byte-identical programs; Go
# downloads that exact version by itself if yours is different.
set -e
cd "$(dirname "$0")"
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.24.7}"
export CGO_ENABLED=0 GOARCH=amd64 GOAMD64=v1 GOFLAGS=
mkdir -p dist
GOOS=windows go build -trimpath -buildvcs=false -ldflags="-s -w" -o dist/cscz-tod-helper.exe .
GOOS=linux go build -trimpath -buildvcs=false -ldflags="-s -w" -o dist/cscz-tod-helper .
(cd dist && sha256sum cscz-tod-helper.exe cscz-tod-helper > SHA256SUMS)
echo "Built with $(go version)"
cat dist/SHA256SUMS
