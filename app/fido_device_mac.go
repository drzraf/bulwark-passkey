//go:build darwin

package main

import (
	virtual_fido "github.com/bulwarkid/virtual-fido"
)

// startFIDODevice serves the authenticator through the virtual USB driver on
// macOS, which does not use USB/IP.
func startFIDODevice(client *FIDOClient) {
	virtual_fido.Start(client)
}
