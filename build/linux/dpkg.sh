#!/bin/bash

# Builds a Debian package from the binary in output/.
#
# Environment variables:
#   VERSION   Package version (default: 0.0.0~dev<date>)
#   ARCH      Debian architecture (default: dpkg --print-architecture)
#   DEB_NAME  Output file name (default: bulwark-passkey_<version>_<arch>.deb)

set -e

BINARY="output/bulwark_passkey"
TEMPLATE_DIR="linux_pkg/bulwark-passkey"
STAGING_DIR="output/deb/bulwark-passkey"

if [[ ! -f $BINARY ]]; then
    echo "$BINARY not found, run 'make build' first" >&2
    exit 1
fi

VERSION="${VERSION:-0.0.0~dev$(date -u +%Y%m%d)}"
ARCH="${ARCH:-$(dpkg --print-architecture)}"
DEB_NAME="${DEB_NAME:-bulwark-passkey_${VERSION}_${ARCH}.deb}"

rm -rf "$(dirname "$STAGING_DIR")"
mkdir -p "$STAGING_DIR"
cp -r "$TEMPLATE_DIR/DEBIAN" "$TEMPLATE_DIR/usr" "$STAGING_DIR/"

# The binary and the icon are not tracked in the template tree, add them here.
mkdir -p "$STAGING_DIR/usr/bin" \
    "$STAGING_DIR/usr/share/icons/hicolor/512x512/apps" \
    "$STAGING_DIR/usr/share/doc/bulwark-passkey"
cp "$BINARY" "$STAGING_DIR/usr/bin/bulwark_passkey"
cp app/build/appicon.png "$STAGING_DIR/usr/share/icons/hicolor/512x512/apps/bulwark-passkey.png"
cp LICENSE "$STAGING_DIR/usr/share/doc/bulwark-passkey/copyright"
chmod 755 "$STAGING_DIR/usr/bin/bulwark_passkey"
chmod 644 "$STAGING_DIR/usr/share/icons/hicolor/512x512/apps/bulwark-passkey.png" \
    "$STAGING_DIR/usr/share/doc/bulwark-passkey/copyright"
find "$STAGING_DIR" -type d -exec chmod 755 {} +

# Stamp the real version and architecture into the control file.
sed -i \
    -e "s|^Version:.*|Version: ${VERSION}|" \
    -e "s|^Architecture:.*|Architecture: ${ARCH}|" \
    "$STAGING_DIR/DEBIAN/control"

INSTALLED_SIZE=$(du -ks "$STAGING_DIR" | cut -f1)
if grep -q '^Installed-Size:' "$STAGING_DIR/DEBIAN/control"; then
    sed -i -e "s|^Installed-Size:.*|Installed-Size: ${INSTALLED_SIZE}|" "$STAGING_DIR/DEBIAN/control"
else
    # Keep Description last, as conventional in a control file.
    sed -i -e "0,/^Description:/s|^Description:|Installed-Size: ${INSTALLED_SIZE}\nDescription:|" \
        "$STAGING_DIR/DEBIAN/control"
fi

if command -v desktop-file-validate >/dev/null; then
    desktop-file-validate "$STAGING_DIR/usr/share/applications/bulwark-passkey.desktop" || true
fi

dpkg-deb --root-owner-group --build "$STAGING_DIR" "output/$DEB_NAME"
rm -rf "$(dirname "$STAGING_DIR")"

if command -v lintian >/dev/null; then
    lintian --no-tag-display-limit "output/$DEB_NAME" || true
fi

echo "Created output/$DEB_NAME"
