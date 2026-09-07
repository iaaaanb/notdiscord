#!/usr/bin/env bash
# Compila un binario estático para Linux y lo instala en el droplet.
#
# Uso:  ./deploy/deploy.sh root@1.2.3.4
set -euo pipefail

TARGET="${1:?uso: ./deploy/deploy.sh usuario@ip}"
BIN=notdiscord
REMOTE_TMP="/tmp/$BIN.new"

cd "$(dirname "$0")/.."

echo "==> compilando (estático, sin cgo)"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -ldflags="-s -w" -o "/tmp/$BIN" ./cmd/server

file "/tmp/$BIN" 2>/dev/null || true
ls -lh "/tmp/$BIN"

echo "==> copiando a $TARGET"
scp "/tmp/$BIN" "$TARGET:$REMOTE_TMP"

# No se puede sobrescribir un binario en ejecución ("text file busy"),
# así que se detiene el servicio, se reemplaza y se levanta.
echo "==> instalando y reiniciando"
ssh "$TARGET" bash -s <<REMOTE
set -euo pipefail
systemctl stop $BIN || true
install -m 755 $REMOTE_TMP /usr/local/bin/$BIN
rm -f $REMOTE_TMP
systemctl start $BIN
sleep 1
systemctl --no-pager --lines=15 status $BIN
REMOTE

echo "==> listo"
