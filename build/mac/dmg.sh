#!/bin/bash

# Packages output/Bulwark Passkey.app into a distributable disk image.
#
# Environment variables:
#   VERSION                  Version string used in the volume name and file name
#   DMG_NAME                 Output file name (default bulwark-passkey-$VERSION-macos-universal.dmg)
#   MACOS_SIGNING_IDENTITY   Codesigning identity used to sign the disk image (optional)

set -e

APP_PATH="output/Bulwark Passkey.app"
VERSION="${VERSION:-dev}"
DMG_NAME="${DMG_NAME:-bulwark-passkey-$VERSION-macos-universal.dmg}"
DMG_PATH="output/$DMG_NAME"
STAGING_DIR="output/dmg-staging"

if [[ ! -d $APP_PATH ]]; then
    echo "$APP_PATH not found, run 'make build' first" >&2
    exit 1
fi

rm -rf "$STAGING_DIR" "$DMG_PATH"
mkdir -p "$STAGING_DIR"
cp -R "$APP_PATH" "$STAGING_DIR/"
ln -s /Applications "$STAGING_DIR/Applications"

hdiutil create \
    -volname "Bulwark Passkey $VERSION" \
    -srcfolder "$STAGING_DIR" \
    -ov -format UDZO \
    -fs HFS+ \
    "$DMG_PATH"

rm -rf "$STAGING_DIR"

if [[ -n ${MACOS_SIGNING_IDENTITY:-} ]]; then
    codesign --force --sign "$MACOS_SIGNING_IDENTITY" --timestamp "$DMG_PATH"
fi

echo "Created $DMG_PATH"
