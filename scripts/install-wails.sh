#!/usr/bin/env bash
set -euo pipefail

WAILS_VERSION=v2.13.0
XTOOLS_VERSION=v0.53.0

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
cd "$dir"
cat > go.mod <<EOF
module wailscli
go $(go env GOVERSION | sed -E 's/^go([0-9]+\.[0-9]+).*/\1/')
EOF
cat > tools.go <<'EOF'
//go:build tools

package tools

import _ "github.com/wailsapp/wails/v2/cmd/wails"
EOF
go get "github.com/wailsapp/wails/v2@${WAILS_VERSION}" "golang.org/x/tools@${XTOOLS_VERSION}"
go mod tidy
out="$(go env GOPATH)/bin/wails$(go env GOEXE)"
go build -o "$out" github.com/wailsapp/wails/v2/cmd/wails
echo "Installed $out"
