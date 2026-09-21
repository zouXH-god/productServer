#!/usr/bin/env sh
set -eu
cd "$(dirname "$0")/frontend"
npm ci
npm test
npm run build
cd ..
go test ./...
mkdir -p bin
go build -trimpath -ldflags='-s -w' -o bin/productserver .
