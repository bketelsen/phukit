#!/bin/bash
# Build script for phukit

set -e

VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo "dev")}
LDFLAGS="-X main.version=$VERSION -s -w"

echo "Building phukit version $VERSION..."

# Build for current platform
go build -ldflags "$LDFLAGS" -o phukit .

echo "Build complete: ./phukit"
echo "Run './phukit --help' to get started"
