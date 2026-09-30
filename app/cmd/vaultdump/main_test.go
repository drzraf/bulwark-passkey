package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bulwarkid/virtual-fido/identities"
	"github.com/bulwarkid/virtual-fido/webauthn"
)

func source(rp, user string, id, key byte) identities.SavedCredentialSource {
	return identities.SavedCredentialSource{
		Type:             "public-key",
		ID:               []byte{id, id, id, id},
		PrivateKey:       []byte{key, key, key, key},
		RelyingParty:     webauthn.PublicKeyCredentialRPEntity{ID: rp, Name: rp},
		User:             webauthn.PublicKeyCrendentialUserEntity{ID: []byte(user), Name: user, DisplayName: user},
		SignatureCounter: 3,
	}
}

// writeVault builds a vault in the current (nested) on-disk format.
func writeVault(t *testing.T, passphrase string, sources []identities.SavedCredentialSource) string {
	t.Helper()
	config := identities.FIDODeviceConfig{
		EncryptionKey:          []byte("0123456789abcdef0123456789abcdef"),
		AttestationCertificate: []byte("cert"),
		AttestationPrivateKey:  []byte("key"),
		AuthenticationCounter:  7,
		Sources:                sources,
	}
	inner, err := identities.EncryptFIDOState(config, passphrase)
	if err != nil {
		t.Fatalf("EncryptFIDOState: %v", err)
	}
	stateBytes, err := json.Marshal(ClientSavedState{VirtualFIDOConfig: inner})
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	outer, err := identities.EncryptWithPassphrase(passphrase, stateBytes)
	if err != nil {
		t.Fatalf("EncryptWithPassphrase: %v", err)
	}
	favicons, err := json.Marshal(map[string]string{"google.com": "data:image/x-icon;base64,AA=="})
	if err != nil {
		t.Fatalf("marshal favicons: %v", err)
	}
	fileBytes, err := json.Marshal(VaultFile{
		VaultType:   "local",
		Data:        outer,
		LastUpdated: "2026-09-30T18:00:00Z",
		Favicons:    favicons,
	})
	if err != nil {
		t.Fatalf("marshal vault: %v", err)
	}
	path := filepath.Join(t.TempDir(), "vault.json")
	if err := os.WriteFile(path, fileBytes, 0600); err != nil {
		t.Fatalf("write vault: %v", err)
	}
	return path
}

func TestDecryptVault(t *testing.T) {
	const passphrase = "correct horse battery staple"
	path := writeVault(t, passphrase, []identities.SavedCredentialSource{
		source("google.com", "alice@example.com", 1, 10),
		source("github.com", "alice", 2, 20),
	})

	d := decryptVault(path, passphrase)

	if d.Format != "current (nested)" {
		t.Errorf("format = %q, want current (nested)", d.Format)
	}
	if d.VaultType != "local" {
		t.Errorf("vault type = %q", d.VaultType)
	}
	if d.AuthenticationCounter != 7 {
		t.Errorf("auth counter = %d, want 7", d.AuthenticationCounter)
	}
	if d.SourceCount != 2 || len(d.Sources) != 2 {
		t.Fatalf("source count = %d, want 2", d.SourceCount)
	}
	if d.Sources[0].RelyingPartyID != "google.com" || d.Sources[0].UserName != "alice@example.com" {
		t.Errorf("source 0 = %+v", d.Sources[0])
	}
	if d.Sources[1].RelyingPartyID != "github.com" {
		t.Errorf("source 1 = %+v", d.Sources[1])
	}
	if len(d.Duplicates) != 0 {
		t.Errorf("unexpected duplicates: %v", d.Duplicates)
	}
	if len(d.FaviconDomains) != 1 || d.FaviconDomains[0] != "google.com" {
		t.Errorf("favicons = %v", d.FaviconDomains)
	}
	// Secret material must not leak into the default dump.
	if !strings.HasPrefix(d.EncryptionKey, "sha256:") {
		t.Errorf("device key not fingerprinted: %q", d.EncryptionKey)
	}
}

// TestDuplicateDetection reproduces the reported corruption shape: the same
// credential repeated several times, crowding out the other entries.
func TestDuplicateDetection(t *testing.T) {
	const passphrase = "hunter2hunter2"
	dup := source("github.com", "alice", 2, 20)
	path := writeVault(t, passphrase, []identities.SavedCredentialSource{dup, dup, dup})

	d := decryptVault(path, passphrase)
	if len(d.Duplicates) == 0 {
		t.Fatal("expected duplicates to be reported")
	}
	joined := strings.Join(d.Duplicates, "\n")
	for _, want := range []string{"credential id", "private key", "relying party + user", "appears 3 times"} {
		if !strings.Contains(joined, want) {
			t.Errorf("duplicates missing %q:\n%s", want, joined)
		}
	}
}

func TestDecryptLegacyVault(t *testing.T) {
	const passphrase = "legacy pass"
	config := identities.FIDODeviceConfig{
		EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		Sources:       []identities.SavedCredentialSource{source("example.com", "bob", 9, 90)},
	}
	blob, err := identities.EncryptFIDOState(config, passphrase)
	if err != nil {
		t.Fatalf("EncryptFIDOState: %v", err)
	}
	fileBytes, err := json.Marshal(VaultFile{VaultType: "local", Data: blob})
	if err != nil {
		t.Fatalf("marshal vault: %v", err)
	}
	path := filepath.Join(t.TempDir(), "vault.json")
	if err := os.WriteFile(path, fileBytes, 0600); err != nil {
		t.Fatalf("write vault: %v", err)
	}

	d := decryptVault(path, passphrase)
	if d.Format != "legacy (single layer)" {
		t.Errorf("format = %q, want legacy", d.Format)
	}
	if d.SourceCount != 1 || d.Sources[0].RelyingPartyID != "example.com" {
		t.Errorf("sources = %+v", d.Sources)
	}
}
