#!/bin/bash

# Environment variables:
#   DEBUG=1                  Build a debug app bundle (devtools enabled)
#   WAILS                    Path to the wails CLI (defaults to $PATH, then ~/go/bin/wails)
#   VERSION                  Version string exposed to the frontend as REACT_APP_VERSION
#   MACOS_SIGNING_IDENTITY   Codesigning identity (name or SHA-1 hash) from the keychain.
#                            When empty the bundle is ad-hoc signed, which is enough to
#                            launch it locally but not to install the system extension.
#   MACOS_PLATFORM           wails -platform value (default darwin/universal)

[[ -n $DEBUG && $DEBUG != 0 ]] && DEBUG=-debug || DEBUG=

set -e

WAILS="${WAILS:-$(command -v wails || echo "$HOME/go/bin/wails")}"
if [[ ! -x $WAILS ]]; then
    echo "wails CLI not found (looked at '$WAILS'), install it with:" >&2
    echo "  go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0" >&2
    exit 1
fi

PLATFORM="${MACOS_PLATFORM:-darwin/universal}"

cd frontend
mkdir -p src/proto || true
REACT_APP_VERSION="${VERSION:-}" npm run build
cd ..
cp -r frontend/build app/frontend_dist

cd app
CONFIG_DIR="build/darwin"
APP_PATH="build/bin/Bulwark Passkey.app"
"$WAILS" build -s -skipbindings -platform "$PLATFORM" $DEBUG
mkdir -p "$APP_PATH/Contents/Library/SystemExtensions" && cp -r "$CONFIG_DIR/id.bulwark.VirtualUSBDriver.driver.dext" "$APP_PATH/Contents/Library/SystemExtensions"
mkdir -p "$APP_PATH/Contents/Frameworks" && cp "mac/installer/libinstaller.dylib" "$APP_PATH/Contents/Frameworks"
cp "$CONFIG_DIR/profile.provisionprofile" "$APP_PATH/Contents/embedded.provisionprofile"
# TODO: See if we can fix the libinstaller.dylib load path before this step
install_name_tool -change libinstaller.dylib @rpath/libinstaller.dylib "$APP_PATH/Contents/MacOS/Bulwark Passkey"
install_name_tool -add_rpath @loader_path/../Frameworks "$APP_PATH/Contents/MacOS/Bulwark Passkey"

if [[ -n ${MACOS_SIGNING_IDENTITY:-} ]]; then
    codesign --force --deep --sign "$MACOS_SIGNING_IDENTITY" --options=runtime \
        --entitlements "$CONFIG_DIR/app.entitlements" --timestamp --verbose \
        --generate-entitlement-der "$APP_PATH"
else
    echo "MACOS_SIGNING_IDENTITY is not set, falling back to ad-hoc signing."
    echo "The resulting bundle cannot install the virtual USB system extension."
    codesign --force --deep --sign - --verbose "$APP_PATH"
fi
cd ..

mkdir -p output && mv "app/$APP_PATH" output/
