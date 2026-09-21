#!/bin/bash

# Environment variables:
#   DEBUG=1      Build a debug binary (devtools enabled)
#   WAILS        Path to the wails CLI (defaults to $PATH, then ~/go/bin/wails)
#   VERSION      Version string exposed to the frontend as REACT_APP_VERSION
#   WAILS_TAGS   Go build tags (default webkit2_41, set to "" for webkit2gtk-4.0)

[[ -n $DEBUG && $DEBUG != 0 ]] && DEBUG=-debug || DEBUG=

set -e

WAILS="${WAILS:-$(command -v wails || echo "$HOME/go/bin/wails")}"
if [[ ! -x $WAILS ]]; then
    echo "wails CLI not found (looked at '$WAILS'), install it with:" >&2
    echo "  go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0" >&2
    exit 1
fi

# webkit2gtk-4.1 (libsoup3) is what current Debian/Ubuntu ship. Distributions
# that only provide the old 4.0 API can build with WAILS_TAGS= .
TAGS=()
WAILS_TAGS="${WAILS_TAGS-webkit2_41}"
[[ -n $WAILS_TAGS ]] && TAGS=(-tags "$WAILS_TAGS")

cd frontend
mkdir -p src/proto || true
REACT_APP_VERSION="${VERSION:-}" npm run build
cd ..

cp -r frontend/build app/frontend_dist
cd app
"$WAILS" build -s -skipbindings "${TAGS[@]}" $DEBUG
cd ..
mkdir -p output
mv app/build/bin/bulwark_passkey output/
