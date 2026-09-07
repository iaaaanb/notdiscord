#!/usr/bin/env bash
# Regenera docs/media/demo.gif.
#
#   ./scripts/record-demo/record.sh
#
# Levanta el servidor con una base vacía, graba dos navegadores con
# Playwright y pega los videos lado a lado. Requiere go, node y ffmpeg.
#
# Variables: WIDTH (ancho del gif, 900), FPS (10), COLORS (64), PORT (8099),
# SPEED (1.4: la toma se reproduce algo más rápida para que el GIF sea corto)
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
WIDTH=${WIDTH:-900}
FPS=${FPS:-10}
COLORS=${COLORS:-64}
PORT=${PORT:-8099}
SPEED=${SPEED:-1.4}

cd "$HERE"
[ -d node_modules ] || { npm install; npx playwright install chromium; }

TMP="$(mktemp -d)"
trap 'kill "${SRV:-}" 2>/dev/null || true; rm -rf "$TMP"' EXIT

echo "==> compilando el servidor"
(cd "$REPO" && go build -o "$TMP/notdiscord" ./cmd/server)

# Base vacía en cada corrida: la demo es reproducible, y el historial que se
# ve en el video es el que se escribe durante la toma.
echo "==> levantando el servidor en :$PORT"
"$TMP/notdiscord" -addr "127.0.0.1:$PORT" -db "$TMP/demo.db" >"$TMP/server.log" 2>&1 &
SRV=$!
for _ in $(seq 1 40); do
	curl -sf -o /dev/null "http://127.0.0.1:$PORT/" && break
	sleep 0.25
done
curl -sf -o /dev/null "http://127.0.0.1:$PORT/" || { cat "$TMP/server.log"; exit 1; }

echo "==> grabando"
DEMO_URL="http://127.0.0.1:$PORT" node record.mjs

# Los dos videos se pegan con hstack, con una franja delgada al medio para
# que se lea como dos ventanas y no como una sola.
# Playwright a veces graba una ventana más baja que su viewport y rellena el
# resto con gris, lo que además se come la barra de escribir. record.mjs lo
# empuja para que no pase, pero es una maña del navegador: si igual pasó, mejor
# fallar que publicar un GIF torcido.
echo "==> revisando que los dos paneles estén completos"
for f in out/ana.webm out/bruno.webm; do
	ffmpeg -y -v error -ss 3 -i "$f" -frames:v 1 -f rawvideo -pix_fmt rgb24 - |
		PANE_W="${PANE_W:-600}" PANE_H="${PANE_H:-620}" VIDEO="$f" python3 -c '
import os, sys
w, h = int(os.environ["PANE_W"]), int(os.environ["PANE_H"])
video = os.environ["VIDEO"]
data = sys.stdin.buffer.read()
gray = bytes((0x80, 0x80, 0x80)) * w
for y in range(h):
    if data[y * w * 3:(y + 1) * w * 3] == gray:
        sys.exit("%s: grabado a %dpx de alto en vez de %d; volvé a correr el script" % (video, y, h))
'
done

#
# El -ss se come el arranque, mientras las dos páginas todavía están cargando:
# es el mismo recorte en los dos, así que no desalinea nada.
echo "==> armando el gif (${WIDTH}px, ${FPS}fps)"
ffmpeg -y -loglevel error -ss "${TRIM:-0.7}" -i out/ana.webm -ss "${TRIM:-0.7}" -i out/bruno.webm -filter_complex "
	[0:v]setpts=PTS/${SPEED},fps=${FPS},pad=iw+6:ih:0:0:color=0x0d0d0f[l];
	[1:v]setpts=PTS/${SPEED},fps=${FPS}[r];
	[l][r]hstack=inputs=2[s];
	[s]scale=${WIDTH}:-1:flags=lanczos,split[s0][s1];
	[s0]palettegen=max_colors=${COLORS}:stats_mode=diff[p];
	[s1][p]paletteuse=dither=bayer:bayer_scale=4" \
	-loop 0 out/demo.gif

mkdir -p "$REPO/docs/media"
cp out/demo.gif "$REPO/docs/media/demo.gif"
ls -lh "$REPO/docs/media/demo.gif"
