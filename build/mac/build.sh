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
# Wails names the .app bundle after `name` in wails.json, but the executable
# inside it after `outputfilename`. It used to use `name` for both, and the
# rename landed between v2.3.1 and v2.9.2, so this path changed when the CLI was
# upgraded. Keep it in sync with CFBundleExecutable in $CONFIG_DIR/Info.plist.
APP_PATH="build/bin/Bulwark Passkey.app"
APP_BINARY="$APP_PATH/Contents/MacOS/bulwark_passkey"
"$WAILS" build -s -skipbindings -platform "$PLATFORM" $DEBUG

# Without this the `mkdir -p` calls below would happily fabricate an empty
# bundle and the failure would only surface later, or not at all.
if [[ ! -f $APP_BINARY ]]; then
    echo "wails did not produce '$APP_BINARY'. Contents of build/bin:" >&2
    find build/bin -mindepth 1 -maxdepth 3 >&2
    exit 1
fi

# A mismatch here still builds, signs and packages, but the app refuses to
# launch, so fail the build instead of shipping it.
BUNDLE_EXECUTABLE="$(plutil -extract CFBundleExecutable raw "$APP_PATH/Contents/Info.plist")"
if [[ $BUNDLE_EXECUTABLE != "$(basename "$APP_BINARY")" ]]; then
    echo "CFBundleExecutable is '$BUNDLE_EXECUTABLE' but the bundled binary is" \
        "'$(basename "$APP_BINARY")'. Fix CFBundleExecutable in $CONFIG_DIR/Info.plist." >&2
    exit 1
fi

mkdir -p "$APP_PATH/Contents/Library/SystemExtensions" && cp -r "$CONFIG_DIR/id.bulwark.VirtualUSBDriver.driver.dext" "$APP_PATH/Contents/Library/SystemExtensions"
mkdir -p "$APP_PATH/Contents/Frameworks" && cp "mac/installer/libinstaller.dylib" "$APP_PATH/Contents/Frameworks"
cp "$CONFIG_DIR/profile.provisionprofile" "$APP_PATH/Contents/embedded.provisionprofile"
# TODO: See if we can fix the libinstaller.dylib load path before this step
install_name_tool -change libinstaller.dylib @rpath/libinstaller.dylib "$APP_BINARY"
install_name_tool -add_rpath @loader_path/../Frameworks "$APP_BINARY"

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
