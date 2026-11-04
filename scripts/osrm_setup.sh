#!/usr/bin/env bash
# osrm_setup.sh: download an OpenStreetMap extract, crop it, prepare it for
# OSRM (MLD) with the car profile, start osrm-routed and smoke-test it.
#
#   scripts/osrm_setup.sh --region austin
#   scripts/osrm_setup.sh --region custom --pbf-url URL --bbox minlng,minlat,maxlng,maxlat
#
# Options:
#   --region NAME     austin (default) or any name with --pbf-url/--bbox
#   --pbf-url URL     extract to download (default for austin: Geofabrik Texas)
#   --bbox BBOX       minlng,minlat,maxlng,maxlat crop box; "none" skips cropping
#   --data-dir DIR    where data goes (default: .osrm-data/ in the repo, git-ignored)
#   --port PORT       osrm-routed port (default 5000)
#   --no-start        prepare only; don't start osrm-routed
#   --force           redo every step even if its output exists
#
# Environment: OSRM_IMAGE (default ghcr.io/project-osrm/osrm-backend:v26.11.0-debian),
# OSMIUM_IMAGE (used only when osmium isn't installed; default
# stefda/osmium-tool), DOCKER (default docker; e.g. "sudo docker"),
# DOCKER_NETWORK=host to run osrm-routed with host networking instead of -p.
#
# Every step is skipped when its output already exists, so rerunning is
# cheap. The download is kept and re-used; delete it or pass --force to fetch
# a newer one. Requires Docker, curl and sha256sum (or shasum).
set -euo pipefail

REGION=austin
PBF_URL=""
BBOX=""
PORT=5000
START=1
FORCE=0
REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
DATA_DIR="$REPO_ROOT/.osrm-data"
OSRM_IMAGE=${OSRM_IMAGE:-ghcr.io/project-osrm/osrm-backend:v26.11.0-debian}
OSMIUM_IMAGE=${OSMIUM_IMAGE:-stefda/osmium-tool}
DOCKER=${DOCKER:-docker}

usage() { sed -n '2,25p' "$0" | sed 's/^# \{0,1\}//'; exit "${1:-0}"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --region) REGION=$2; shift 2 ;;
    --pbf-url) PBF_URL=$2; shift 2 ;;
    --bbox) BBOX=$2; shift 2 ;;
    --data-dir) DATA_DIR=$2; shift 2 ;;
    --port) PORT=$2; shift 2 ;;
    --no-start) START=0; shift ;;
    --force) FORCE=1; shift ;;
    -h|--help) usage 0 ;;
    *) echo "osrm_setup: unknown option $1" >&2; usage 2 ;;
  esac
done

case "$REGION" in
  austin)
    PBF_URL=${PBF_URL:-https://download.geofabrik.de/north-america/us/texas-latest.osm.pbf}
    # Covers every origin, destination and supply location in
    # benchmarks/austin_weekday_10k (lng -98.084..-97.436, lat 30.005..30.716)
    # plus about 0.1 degree (roughly 10 km) of margin on every side.
    BBOX=${BBOX:--98.20,29.90,-97.33,30.82}
    ;;
  *)
    if [[ -z "$PBF_URL" || -z "$BBOX" ]]; then
      echo "osrm_setup: region $REGION needs --pbf-url and --bbox (or --bbox none)" >&2
      exit 2
    fi
    ;;
esac

log() { echo "osrm_setup: $*" >&2; }
need() { command -v "$1" >/dev/null 2>&1 || { echo "osrm_setup: $1 is required" >&2; exit 1; }; }
need curl
$DOCKER info >/dev/null 2>&1 || { echo "osrm_setup: Docker isn't running (tried '$DOCKER info'); install and start Docker, or set DOCKER=\"sudo docker\"" >&2; exit 1; }
sha256() { if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }

REGION_DIR="$DATA_DIR/$REGION"
mkdir -p "$REGION_DIR"
SOURCE_FILE="$REGION_DIR/$(basename "$PBF_URL")"
MAP="$REGION_DIR/$REGION.osm.pbf"
BASE="$REGION_DIR/$REGION.osrm"
META="$REGION_DIR/metadata.json"
CONTAINER="rhs-osrm-$REGION"

# 1. Download.
if [[ $FORCE == 1 || ! -s "$SOURCE_FILE" ]]; then
  log "downloading $PBF_URL"
  curl -fL --retry 3 -o "$SOURCE_FILE.part" "$PBF_URL"
  mv "$SOURCE_FILE.part" "$SOURCE_FILE"
  date -u +%Y-%m-%d > "$SOURCE_FILE.downloaded"
  rm -f "$MAP" "$BASE"*
else
  log "using existing $SOURCE_FILE"
fi
DOWNLOAD_DATE=$(cat "$SOURCE_FILE.downloaded" 2>/dev/null || date -u -r "$SOURCE_FILE" +%Y-%m-%d)

# 2. Crop.
if [[ $FORCE == 1 || ! -s "$MAP" || "$(cat "$MAP.bbox" 2>/dev/null)" != "$BBOX" ]]; then
  rm -f "$BASE"*
  if [[ "$BBOX" == none ]]; then
    cp "$SOURCE_FILE" "$MAP"
  elif command -v osmium >/dev/null; then
    log "cropping to $BBOX with osmium"
    osmium extract --overwrite -s smart -b "$BBOX" -o "$MAP" "$SOURCE_FILE"
  else
    log "cropping to $BBOX with $OSMIUM_IMAGE"
    $DOCKER run --rm -v "$REGION_DIR:/data" "$OSMIUM_IMAGE" \
      osmium extract --overwrite -s smart -b "$BBOX" -o "/data/$(basename "$MAP")" "/data/$(basename "$SOURCE_FILE")"
  fi
  echo "$BBOX" > "$MAP.bbox"
fi
SHA=$(sha256 "$MAP")

# 3. Prepare for MLD, which can take traffic updates (osrm-customize
# --segment-speed-file) without a full rebuild.
run_osrm() { $DOCKER run --rm -v "$REGION_DIR:/data" "$OSRM_IMAGE" "$@"; }
if [[ ! -s "$BASE.mldgr" ]]; then
  log "osrm-extract (car profile)"
  run_osrm osrm-extract -p /opt/car.lua "/data/$REGION.osm.pbf"
  log "osrm-partition"
  run_osrm osrm-partition "/data/$REGION.osrm"
  log "osrm-customize"
  run_osrm osrm-customize "/data/$REGION.osrm"
else
  log "using existing prepared map $BASE"
fi
OSRM_VERSION=$(run_osrm osrm-routed --version 2>/dev/null | tr -d '\r' | tail -1)

# 4. Metadata. The SHA-256 is of the cropped .osm.pbf OSRM was built from.
cat > "$META" <<JSON
{
  "region": "$REGION",
  "name": "$REGION ($(basename "$PBF_URL"), cropped)",
  "source_url": "$PBF_URL",
  "download_date": "$DOWNLOAD_DATE",
  "bbox": "$BBOX",
  "pbf": "$(basename "$MAP")",
  "sha256": "$SHA",
  "osrm_image": "$OSRM_IMAGE",
  "osrm_version": "$OSRM_VERSION",
  "profile": "car",
  "algorithm": "mld"
}
JSON
log "wrote $META"

# 5. Start osrm-routed.
URL="http://127.0.0.1:$PORT"
if [[ $START == 1 ]]; then
  if $DOCKER ps --format '{{.Names}}' | grep -qx "$CONTAINER"; then
    log "$CONTAINER is already running"
  else
    $DOCKER rm -f "$CONTAINER" >/dev/null 2>&1 || true
    if [[ "${DOCKER_NETWORK:-}" == host ]]; then
      net=(--network host)
    else
      net=(-p "127.0.0.1:$PORT:$PORT")
    fi
    log "starting osrm-routed on $URL"
    $DOCKER run -d --name "$CONTAINER" "${net[@]}" -v "$REGION_DIR:/data" "$OSRM_IMAGE" \
      osrm-routed --algorithm mld --port "$PORT" --max-table-size 10000 "/data/$REGION.osrm" >/dev/null
  fi
  # 6. Smoke test: a route across the middle of the box.
  IFS=, read -r minlng minlat maxlng maxlat <<<"${BBOX/none/-180,-90,180,90}"
  a=$(awk -v a="$minlng" -v b="$maxlng" -v c="$minlat" -v d="$maxlat" 'BEGIN{printf "%.5f,%.5f", a+(b-a)*0.4, c+(d-c)*0.4}')
  b=$(awk -v a="$minlng" -v b="$maxlng" -v c="$minlat" -v d="$maxlat" 'BEGIN{printf "%.5f,%.5f", a+(b-a)*0.6, c+(d-c)*0.6}')
  for i in $(seq 1 60); do
    if out=$(curl -fsS "$URL/route/v1/car/$a;$b?overview=false" 2>/dev/null) && [[ "$out" == *'"code":"Ok"'* ]]; then
      log "smoke test OK: $a -> $b: $(echo "$out" | grep -o '"duration":[0-9.]*' | head -1), $(echo "$out" | grep -o '"distance":[0-9.]*' | head -1)"
      break
    fi
    [[ $i == 60 ]] && { echo "osrm_setup: osrm-routed didn't answer at $URL; see '$DOCKER logs $CONTAINER'" >&2; exit 1; }
    sleep 1
  done
fi

cat <<TOML

# Add to your settings file:
[hooks]
routing = "osrm"

[osrm]
url = "$URL"
profile = "car"
dataset_metadata_path = "$META"
# cache_path = "osrm_cache.jsonl"   # optional: rerun without OSRM
TOML
