#!/bin/bash
set -euo pipefail

VERSION="${1:-dev}"
GOOS_TARGET="${2:-$(go env GOOS)}"
GOARCH_TARGET="${3:-$(go env GOARCH)}"
LDFLAGS="-s -w -X main.version=${VERSION}"
NAME="wcaw-${GOOS_TARGET}-${GOARCH_TARGET}"

echo "Building ${NAME} (version ${VERSION})..."
CGO_ENABLED=1 GOOS="${GOOS_TARGET}" GOARCH="${GOARCH_TARGET}" \
  go build -ldflags "${LDFLAGS}" -o wcaw ./cmd/wcaw

tar -czf "${NAME}.tar.gz" wcaw
rm wcaw
echo "Done: ${NAME}.tar.gz"
