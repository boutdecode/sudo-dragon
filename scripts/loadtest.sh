#!/usr/bin/env bash
# Test de charge reproductible: lance un serveur éphémère (base temporaire),
# crée les comptes des bots, puis mesure la cadence du serveur en régime établi.
#
# Usage: scripts/loadtest.sh [N_BOTS] [DURÉE_MESURE_S]      ex.: scripts/loadtest.sh 250 30
# Variables: GO (défaut: go), PORT (défaut: 18080), LOGIN_WORKERS (défaut: 8)
#
# Les bots tournent sur la même machine que le serveur et lui prennent du CPU:
# les chiffres sont donc pessimistes par rapport à un serveur dédié.
set -euo pipefail

N="${1:-100}"
MEASURE="${2:-30}"
GO="${GO:-go}"
PORT="${PORT:-18080}"
WORKERS="${LOGIN_WORKERS:-8}"

cd "$(dirname "$0")/.."
WORK="$(mktemp -d)"
SERVER_PID=""
cleanup() {
	[[ -n "$SERVER_PID" ]] && kill -TERM "$SERVER_PID" 2>/dev/null && wait "$SERVER_PID" 2>/dev/null || true
	rm -rf "$WORK"
}
trap cleanup EXIT

echo "Compilation…"
"$GO" build -o "$WORK/server" ./cmd/server
"$GO" build -o "$WORK/bot" ./cmd/bot

"$WORK/server" -addr "127.0.0.1:$PORT" -db "$WORK/load.db" \
	-max-registrations 0 -login-workers "$WORKERS" 2>"$WORK/server.log" &
SERVER_PID=$!
for _ in $(seq 1 50); do
	curl -fs "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1 && break
	sleep 0.1
done
curl -fs "http://127.0.0.1:$PORT/healthz" >/dev/null || { echo "le serveur n'a pas démarré"; cat "$WORK/server.log"; exit 1; }

URL="ws://127.0.0.1:$PORT/ws"
echo "Création de $N comptes de bots…"
"$WORK/bot" -addr "$URL" -n "$N" -d 12s >/dev/null 2>&1 || true
sleep 1

echo "Mesure: $N bots regroupés au point de départ, régime établi ($MEASURE s après 25 s de chauffe)…"
WARMUP=25
"$WORK/bot" -addr "$URL" -n "$N" -d "$((WARMUP + MEASURE))s" -warmup "${WARMUP}s" -min-tick-rate 9

WARN="$(grep -c 'WARN\|ERROR' "$WORK/server.log" || true)"
echo "avertissements/erreurs du serveur: $WARN"
