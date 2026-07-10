#!/bin/bash
set -e

# Deploy grocer to luiscup.
#
# Modeled on transaction-summary/deploy.sh. The SSH target is a
# Host alias from ~/.ssh/config. The service runs under the user's
# systemd (no root needed).
#
# Prereqs (one-time, on the server):
#   1. systemd --user service file at
#      ~/.config/systemd/user/grocer.service
#   2. GCS credentials at
#      ~/.config/grocer/credentials.json
#   3. Photo cache dir at
#      ~/cache/grocer/photos
#   4. traefik route for grocer.sirenko.ca -> the service port
#      (default 8083, set in the service file's PORT env var)

SERVER="luiscup"
SSH="ssh $SERVER"
SCP="scp"
REMOTE_BIN="~/bin/grocer-server"
SERVICE="grocer"

echo "Building frontend (prod, embeds into binary)..."
rm -rf dist
bun build --outdir=dist --production ./client/index.html

echo "Building binary (with embedded assets)..."
GIT_COMMIT=$(git rev-parse --short HEAD)
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS="-s -w -X 'main.GitCommit=$GIT_COMMIT' -X 'main.BuildTime=$BUILD_TIME'"
GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o grocer-server ./cmd/server/main.go
echo "  -> ${GIT_COMMIT} (${BUILD_TIME})"

echo "Uploading binary..."
$SCP grocer-server $SERVER:/tmp/grocer-server

echo "Deploying..."
$SSH "systemctl --user stop $SERVICE 2>/dev/null; sleep 1; \
  mkdir -p ~/bin && \
  cp /tmp/grocer-server $REMOTE_BIN && chmod +x $REMOTE_BIN && \
  systemctl --user start $SERVICE"

echo "Done! Status:"
$SSH "systemctl --user status $SERVICE --no-pager | head -15"
