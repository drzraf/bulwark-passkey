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

## Attaching the authenticator on Linux

The app serves the virtual key over USB/IP on `127.0.0.1:3240` and asks the
kernel to plug it in, at startup and again whenever the host detaches it. Two
privileged commands are involved, each executed directly, never through a
shell, so that sudoers, polkit and AppArmor rules can match the exact program
and arguments:

```
modprobe vhci-hcd                          # only when the driver is missing
usbip attach -r 127.0.0.1 -b 2-2
```

Each command is tried in the order that disturbs the user least: run directly
when the app already runs as root, then `sudo -n`, which stays silent unless a
`NOPASSWD` rule covers it, and finally `pkexec`, which asks through the
desktop's polkit agent.

`modprobe` is skipped when `/sys/devices/platform/vhci_hcd.0` exists or
`/proc/modules` lists `vhci_hcd`, so a built-in driver, one loaded by udev, or
one already loaded by a previous attach costs nothing. Loading it once per boot
avoids the question entirely:

```
echo vhci-hcd | sudo tee /etc/modules-load.d/vhci-hcd.conf
```

### Attaching without a password prompt

Either mechanism works; pick the one your system already uses. Check the paths
first, since `usbip` lives in `/usr/bin`, `/usr/sbin` or `/usr/local/bin`
depending on the distribution:

```
command -v usbip modprobe
```

A sudoers drop-in, edited with `sudo visudo -f /etc/sudoers.d/bulwark-passkey`,
covers the `sudo -n` step:

```
yug ALL=(root) NOPASSWD: /usr/local/bin/usbip attach -r 127.0.0.1 -b 2-2
yug ALL=(root) NOPASSWD: /usr/sbin/modprobe vhci-hcd
```

A polkit rule in `/etc/polkit-1/rules.d/49-bulwark-passkey.rules` covers the
`pkexec` step instead, and can match the whole command line:

```javascript
polkit.addRule(function (action, subject) {
    if (action.id !== "org.freedesktop.policykit.exec") {
        return polkit.Result.NOT_HANDLED;
    }
    var command = action.lookup("command_line");
    if (subject.isInGroup("users") &&
        (command === "/usr/local/bin/usbip attach -r 127.0.0.1 -b 2-2" ||
         command === "/usr/sbin/modprobe vhci-hcd")) {
        return polkit.Result.YES;
    }
    return polkit.Result.NOT_HANDLED;
});
```

Both grant root for those two exact commands only, which is why the app never
runs `pkexec bash -c ...`: allowing a shell to run as root would grant far more
than attaching a USB device.

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

`.github/workflows/release.yml` builds Linux (`.deb` + portable zipped binary)
and macOS (universal `.dmg`) artifacts. It only runs when there is something to
release:

* a new commit on `master` refreshes the rolling `nightly` pre-release; pushes
  that only touch `marketing-next/` or documentation are skipped;
* pushing a `v*` tag publishes a normal release with the same artifacts plus
  `SHA256SUMS`;
* `workflow_dispatch` rebuilds on demand, with an input to choose whether the
  result is published.

To skip a run, put `[skip ci]` in the commit message (`[ci skip]`, `[no ci]`,
`[skip actions]` and `[actions skip]` work too). GitHub applies this to `push`
and `pull_request` events, so it covers both `master` commits and `v*` tags, but
a `workflow_dispatch` run cannot be skipped this way.

Each asset is uploaded with `archive: false`, so the workflow artifacts are the
real files rather than one zip containing several of them. The portable binary
is zipped rather than `tar.gz`'d: a single file does not need tar, and a zip
still records the executable bit that a bare `.gz` would drop.

Linux artifacts are built on a pinned `ubuntu-24.04` image against
`webkit2gtk-4.1`, and the package depends on the runtime libraries through
alternatives (`libgtk-3-0t64 | libgtk-3-0`, ...) so it installs on both the
pre- and post-`t64` distributions. `usbip` and `polkit` are `Recommends`, which
apt installs by default while keeping the package installable where those
package names differ. The runner image sets the glibc floor, currently 2.39, so
the binaries do not run on older distributions such as Debian 12; build locally
for those. Both Linux artifacts are dynamically linked against GTK 3 and
WebKitGTK, which cannot be statically linked because WebKit2 runs its web,
network and GPU work in separate helper executables.

macOS signing and notarization are optional and driven by repository secrets;
when they are missing, the disk image is still produced with an ad-hoc signature:

| Secret | Purpose |
| --- | --- |
| `MACOS_CERTIFICATE` | base64 encoded Developer ID `.p12` |
| `MACOS_CERTIFICATE_PASSWORD` | password of the `.p12` |
| `MACOS_SIGNING_IDENTITY` | identity name to pass to `codesign` |
| `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD` | `notarytool` credentials |
