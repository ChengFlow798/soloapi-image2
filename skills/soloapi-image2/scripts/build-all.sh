#!/bin/sh
set -eu

cd "$(dirname "$0")/src"

go vet ./...
go test ./...

GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o ../bin/soloapi-image2-windows-amd64.exe .
GOOS=windows GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o ../bin/soloapi-image2-windows-arm64.exe .
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o ../bin/soloapi-image2-linux-amd64 .
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o ../bin/soloapi-image2-linux-arm64 .
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o ../bin/soloapi-image2-darwin-amd64 .
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o ../bin/soloapi-image2-darwin-arm64 .
