# Bulwark Passkey

This is the repo for the frontend of [Bulwark Passkey](https://bulwark.id).

## Build Linux

### Requirement

You need to install before :
* go stack
* npm stack
* [protobuf binary](https://github.com/protocolbuffers/protobuf)

### Generic

To build generic packages
```
make build
```
Binary is build in output/ directory

### Debian Sid

```
#Defaut package ecosystem
sudo apt install golang-go npm
#Wails and its requirements
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev
go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0
export PATH=$PATH:~/go/bin

#Protocol Buffers and its requirements
sudo apt install protobuf-compiler protoc-gen-go

#Building process
git clone https://github.com/bulwarkid/bulwark-passkey
cd bulwark-passkey/frontend
npm install
cd ..
```
To build only the binary
```
make build
```
Binary is available at output/bulwark-passkey.deb

To package in a compliant package Debian
```
make build dpkg
```

Package is available at `output/bulwark-passkey_<version>_<arch>.deb`. The version,
architecture and file name can be overridden with the `VERSION`, `ARCH` and
`DEB_NAME` environment variables.

The build uses the `webkit2gtk-4.1` API (libsoup3), which is what Debian 12+ and
Ubuntu 22.04+ ship. On a distribution that only provides the legacy 4.0 API,
build against it with:

```
WAILS_TAGS= make build
```

## Build macOS

```
make build          # produces "output/Bulwark Passkey.app"
make dmg            # produces output/bulwark-passkey-<version>-macos-universal.dmg
```

Without `MACOS_SIGNING_IDENTITY` the bundle is only ad-hoc signed: it launches
locally, but it cannot install the virtual USB system extension. Set
`MACOS_SIGNING_IDENTITY` to a certificate name or SHA-1 hash present in the
keychain to get a properly signed build.

## Releases

`.github/workflows/release.yml` builds Linux (`.deb` + portable tarball) and
macOS (universal `.dmg`) artifacts. It only runs when there is something to
release:

* a new commit on `master` refreshes the rolling `nightly` pre-release; pushes
  that only touch `marketing-next/` or documentation are skipped;
* pushing a `v*` tag publishes a normal release with the same artifacts plus
  `SHA256SUMS`;
* `workflow_dispatch` rebuilds on demand, with an input to choose whether the
  result is published.

Linux artifacts are built on the current `ubuntu-latest` image against
`webkit2gtk-4.1`, and the package depends on the runtime libraries through
alternatives (`libgtk-3-0t64 | libgtk-3-0`, ...) so it installs on both the
pre- and post-`t64` distributions. `usbip` and `polkit` are `Recommends`, which
apt installs by default while keeping the package installable where those
package names differ.

macOS signing and notarization are optional and driven by repository secrets;
when they are missing, the disk image is still produced with an ad-hoc signature:

| Secret | Purpose |
| --- | --- |
| `MACOS_CERTIFICATE` | base64 encoded Developer ID `.p12` |
| `MACOS_CERTIFICATE_PASSWORD` | password of the `.p12` |
| `MACOS_SIGNING_IDENTITY` | identity name to pass to `codesign` |
| `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD` | `notarytool` credentials |
