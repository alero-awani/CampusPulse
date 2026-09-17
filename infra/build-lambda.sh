#!/bin/sh
# Compiles the Go API for AWS Lambda (Linux, ARM64). Run before `terraform apply`
# whenever the backend code changes.
set -eu
cd "$(dirname "$0")/../backend"
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -trimpath -ldflags="-s -w" -o dist/lambda/bootstrap ./cmd/lambda
echo "Built backend/dist/lambda/bootstrap"
