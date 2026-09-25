package main

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bulwarkid/virtual-fido/cose"
	"github.com/bulwarkid/virtual-fido/identities"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Key-level passkey sharing.
//
// A single credential is written out as a small JSON file that can be handed to
// a teammate, who imports it into their own vault. The file contains the
// credential private key in the clear, so it must be transferred over a trusted
// channel; the UI warns about this before exporting.

const sharedPasskeyFormat = "bulwark-passkey-share"

// Version 2 stores credential private keys in the COSE encoding that
// virtual-fido writes; version 1 files used the raw EC encoding and are still
// accepted on import.
const sharedPasskeyVersion = 2
const sharedPasskeyExtension = ".passkey"

type SharedPasskeyFile struct {
	Format  string                           `json:"format"`
	Version int                              `json:"version"`
	Passkey identities.SavedCredentialSource `json:"passkey"`
}

// shareResult is returned to the frontend for both export and import.
type shareResult struct {
	Ok       bool   `json:"ok"`
	Canceled bool   `json:"canceled"`
	Message  string `json:"message"`
}

func shareFailure(format string, args ...interface{}) shareResult {
	message := fmt.Sprintf(format, args...)
	errorf("Passkey sharing error: %s", message)
	return shareResult{Ok: false, Canceled: false, Message: message}
}

func shareCanceled() shareResult {
	return shareResult{Ok: false, Canceled: true, Message: ""}
}

func shareSuccess(format string, args ...interface{}) shareResult {
	return shareResult{Ok: true, Canceled: false, Message: fmt.Sprintf(format, args...)}
}

// exportIdentity writes the credential with the given ID to a user-chosen file.
func (client *Client) exportIdentity(id []byte) shareResult {
	var passkey *identities.SavedCredentialSource
	for _, source := range client.fidoClient.vault.Export() {
		if bytes.Equal(source.ID, id) {
			found := source
			passkey = &found
			break
		}
	}
	if passkey == nil {
		return shareFailure("This passkey could not be found in the vault.")
	}
	file := SharedPasskeyFile{
		Format:  sharedPasskeyFormat,
		Version: sharedPasskeyVersion,
		Passkey: *passkey,
	}
	data, err := json.MarshalIndent(&file, "", "  ")
	if err != nil {
		return shareFailure("Could not serialize the passkey: %v", err)
	}
	path, err := runtime.SaveFileDialog(app.ctx, runtime.SaveDialogOptions{
		Title:                "Export Passkey",
		DefaultFilename:      sharedPasskeyFilename(passkey),
		Filters:              sharedPasskeyFilters(),
		CanCreateDirectories: true,
	})
	if err != nil {
		return shareFailure("Could not open the save dialog: %v", err)
	}
	if path == "" {
		return shareCanceled()
	}
	if filepath.Ext(path) == "" {
		path += sharedPasskeyExtension
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return shareFailure("Could not write the passkey file: %v", err)
	}
	return shareSuccess("Passkey exported to %s. Anyone with this file can log in as this user, so share it only over a trusted channel.", path)
}

// importIdentity reads a shared passkey file and adds it to the vault.
func (client *Client) importIdentity() shareResult {
	path, err := runtime.OpenFileDialog(app.ctx, runtime.OpenDialogOptions{
		Title:   "Import Passkey",
		Filters: sharedPasskeyFilters(),
	})
	if err != nil {
		return shareFailure("Could not open the file dialog: %v", err)
	}
	if path == "" {
		return shareCanceled()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return shareFailure("Could not read the passkey file: %v", err)
	}
	var file SharedPasskeyFile
	if err := json.Unmarshal(data, &file); err != nil {
		return shareFailure("This is not a valid passkey file.")
	}
	if file.Format != sharedPasskeyFormat {
		return shareFailure("This is not a Bulwark passkey file.")
	}
	if file.Version > sharedPasskeyVersion {
		return shareFailure("This passkey file was made by a newer version of Bulwark Passkey.")
	}
	passkey := file.Passkey
	if len(passkey.ID) == 0 {
		return shareFailure("This passkey file is missing a credential ID.")
	}
	if !validSharedPrivateKey(passkey.PrivateKey) {
		return shareFailure("This passkey file has an invalid private key.")
	}
	if passkey.Type == "" {
		passkey.Type = "public-key"
	}
	for _, existing := range client.identities() {
		if bytes.Equal(existing.ID, passkey.ID) {
			return shareFailure("This passkey is already in your vault.")
		}
	}
	if err := importSharedPasskey(client, passkey); err != nil {
		return shareFailure("Could not import the passkey: %v", err)
	}
	client.FIDOUpdated()
	return shareSuccess("Imported the passkey for %s.", sharedPasskeyDescription(&passkey))
}

func sharedPasskeyFilters() []runtime.FileFilter {
	return []runtime.FileFilter{
		{DisplayName: "Bulwark Passkey (*.passkey)", Pattern: "*" + sharedPasskeyExtension},
		{DisplayName: "All Files (*.*)", Pattern: "*.*"},
	}
}

// validSharedPrivateKey accepts both key encodings the vault has used: COSE,
// written by current versions, and the older raw SEC1/x509 EC encoding.
func validSharedPrivateKey(privateKey []byte) bool {
	if parseCOSEPrivateKey(privateKey) == nil {
		return true
	}
	_, err := x509.ParseECPrivateKey(privateKey)
	return err == nil
}

// parseCOSEPrivateKey reports whether the key decodes as COSE. virtual-fido
// panics on partially valid COSE data instead of returning an error, and
// passkey files come from outside the app, so the panic is turned back into
// one.
func parseCOSEPrivateKey(privateKey []byte) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("invalid COSE private key: %v", recovered)
		}
	}()
	_, err = cose.UnmarshalCOSEPrivateKey(privateKey)
	return err
}

// importSharedPasskey adds a passkey from a file to the vault, turning the
// panics virtual-fido raises on malformed key data into a reported failure.
func importSharedPasskey(client *Client, passkey identities.SavedCredentialSource) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("invalid passkey data: %v", recovered)
		}
	}()
	return client.fidoClient.vault.Import([]identities.SavedCredentialSource{passkey})
}

func sharedPasskeyDescription(passkey *identities.SavedCredentialSource) string {
	website := firstNonEmpty(passkey.RelyingParty.Name, passkey.RelyingParty.ID)
	user := firstNonEmpty(passkey.User.DisplayName, passkey.User.Name)
	if website == "" && user == "" {
		return "an unnamed account"
	}
	if user == "" {
		return website
	}
	if website == "" {
		return user
	}
	return fmt.Sprintf("%s on %s", user, website)
}

func sharedPasskeyFilename(passkey *identities.SavedCredentialSource) string {
	parts := make([]string, 0, 2)
	for _, part := range []string{passkey.RelyingParty.ID, passkey.User.Name} {
		if cleaned := sanitizeFilenamePart(part); cleaned != "" {
			parts = append(parts, cleaned)
		}
	}
	if len(parts) == 0 {
		return "passkey" + sharedPasskeyExtension
	}
	return strings.Join(parts, "-") + sharedPasskeyExtension
}

func sanitizeFilenamePart(part string) string {
	var builder strings.Builder
	for _, char := range part {
		switch {
		case char >= 'a' && char <= 'z',
			char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9',
			char == '-', char == '_', char == '.':
			builder.WriteRune(char)
		default:
			builder.WriteRune('-')
		}
	}
	return strings.Trim(builder.String(), "-.")
}
