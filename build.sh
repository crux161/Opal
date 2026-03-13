#!/bin/sh

echo "building for linux amd64/arm64..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/opal-linux-amd64 ./cmd/opal
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o dist/opal-linux-arm64 ./cmd/opal
