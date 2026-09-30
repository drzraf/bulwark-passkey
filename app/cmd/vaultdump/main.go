// vaultdump decrypts a Bulwark Passkey vault file and dumps its contents,
// so that two vaults (e.g. a live vault and a backup) can be compared.
//
//	go build -o vaultdump ./cmd/vaultdump
//	./vaultdump -p 'my passphrase'                      # dump default vault as JSON
//	./vaultdump -p 'my passphrase' -summary             # one line per passkey
//	./vaultdump -p 'pass' -f a.json > a.txt
//	./vaultdump -p 'pass' -f b.json > b.txt && diff a.txt b.txt
//
// Secret key material is replaced by a "sha256:<prefix>" fingerprint unless
// -secrets is given, so that dumps stay diffable without leaking private keys.
package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/bulwarkid/virtual-fido/identities"
)

// VaultFile mirrors app.VaultFile (see app/file_storage.go).
type VaultFile struct {
	VaultType   string `json:"type"`
	Data        []byte `json:"data"`
	LastUpdated string `json:"last_updated"`
	Email       string `json:"email"`
	Favicons    []byte `json:"favicons,omitempty"`
}

// ClientSavedState mirrors app.ClientSavedState (see app/client.go).
// It has no JSON tags in the app either, so field names are used verbatim.
type ClientSavedState struct {
	VirtualFIDOConfig []byte
	DeletedSources    [][]byte
}

type dumpSource struct {
	Index            int    `json:"index"`
	Type             string `json:"type"`
	ID               string `json:"id"`
	RelyingPartyID   string `json:"relying_party_id"`
	RelyingPartyName string `json:"relying_party_name"`
	UserID           string `json:"user_id"`
	UserName         string `json:"user_name"`
	UserDisplayName  string `json:"user_display_name"`
	SignatureCounter int32  `json:"signature_counter"`
	PrivateKey       string `json:"private_key"`
}

type dump struct {
	File                  string       `json:"file"`
	VaultType             string       `json:"vault_type"`
	LastUpdated           string       `json:"last_updated"`
	Email                 string       `json:"email"`
	Format                string       `json:"format"`
	FaviconDomains        []string     `json:"favicon_domains"`
	DeletedSources        []string     `json:"deleted_sources"`
	AuthenticationCounter uint32       `json:"authentication_counter"`
	PINEnabled            bool         `json:"pin_enabled"`
	PINHash               string       `json:"pin_hash"`
	EncryptionKey         string       `json:"device_encryption_key"`
	AttestationCert       string       `json:"attestation_certificate"`
	AttestationKey        string       `json:"attestation_private_key"`
	SourceCount           int          `json:"source_count"`
	Sources               []dumpSource `json:"sources"`
	Duplicates            []string     `json:"duplicates,omitempty"`
}

var showSecrets bool

func main() {
	var (
		passphrase = ""
		path       = ""
		summary    = false
	)

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-p", "--passphrase":
			i++
			if i >= len(args) {
				fatal("missing value for %s", args[i-1])
			}
			passphrase = args[i]
		case "-f", "--file":
			i++
			if i >= len(args) {
				fatal("missing value for %s", args[i-1])
			}
			path = args[i]
		case "-summary":
			summary = true
		case "-secrets":
			showSecrets = true
		case "-h", "--help":
			usage()
			return
		default:
			if passphrase == "" {
				passphrase = args[i] // allow bare positional passphrase
			} else {
				fatal("unknown argument: %s", args[i])
			}
		}
	}

	if passphrase == "" {
		usage()
		os.Exit(2)
	}
	if path == "" {
		path = defaultVaultPath()
	}

	d := decryptVault(path, passphrase)

	if summary {
		printSummary(d)
		return
	}
	out, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		fatal("could not marshal output: %v", err)
	}
	fmt.Println(string(out))
}

func usage() {
	fmt.Fprint(os.Stderr, `vaultdump - decrypt and dump a Bulwark Passkey vault

usage: vaultdump -p <passphrase> [-f <vault.json>] [-summary] [-secrets]

  -p, --passphrase  vault passphrase (required)
  -f, --file        vault file (default: `+defaultVaultPath()+`)
  -summary          print one compact line per passkey instead of JSON
  -secrets          include real private keys instead of sha256 fingerprints

Comparing a vault with a backup:
  vaultdump -p 'pass' -f vault.json    -summary > /tmp/now.txt
  vaultdump -p 'pass' -f vault.bak.json -summary > /tmp/bak.txt
  diff -u /tmp/bak.txt /tmp/now.txt
`)
}

func defaultVaultPath() string {
	root, err := os.UserConfigDir()
	if err != nil {
		return "vault.json"
	}
	return filepath.Join(root, "Bulwark Passkey", "vault.json")
}

func decryptVault(path, passphrase string) *dump {
	raw, err := os.ReadFile(path)
	if err != nil {
		fatal("could not read %s: %v", path, err)
	}
	var vf VaultFile
	if err := json.Unmarshal(raw, &vf); err != nil {
		fatal("%s is not a vault file: %v", path, err)
	}

	d := &dump{
		File:        path,
		VaultType:   vf.VaultType,
		LastUpdated: vf.LastUpdated,
		Email:       vf.Email,
	}

	if len(vf.Favicons) > 0 {
		cache := map[string]string{}
		if err := json.Unmarshal(vf.Favicons, &cache); err == nil {
			for domain := range cache {
				d.FaviconDomains = append(d.FaviconDomains, domain)
			}
			sort.Strings(d.FaviconDomains)
		}
	}

	plaintext, err := identities.DecryptWithPassphrase(passphrase, vf.Data)
	if err != nil {
		fatal("could not decrypt vault (wrong passphrase?): %v", err)
	}

	// The app supports two layouts; detect the same way loadData() does.
	var config *identities.FIDODeviceConfig
	var legacy identities.FIDODeviceConfig
	if err := json.Unmarshal(plaintext, &legacy); err == nil && legacy.EncryptionKey != nil {
		d.Format = "legacy (single layer)"
		config = &legacy
	} else {
		var state ClientSavedState
		if err := json.Unmarshal(plaintext, &state); err != nil {
			fatal("could not parse saved state: %v", err)
		}
		config, err = identities.DecryptFIDOState(state.VirtualFIDOConfig, passphrase)
		if err != nil {
			fatal("could not decrypt inner FIDO state: %v", err)
		}
		d.Format = "current (nested)"
		for _, src := range state.DeletedSources {
			d.DeletedSources = append(d.DeletedSources, b64(src))
		}
	}

	d.AuthenticationCounter = config.AuthenticationCounter
	d.PINEnabled = config.PINEnabled
	d.PINHash = fingerprint(config.PINHash)
	d.EncryptionKey = fingerprint(config.EncryptionKey)
	d.AttestationCert = fingerprint(config.AttestationCertificate)
	d.AttestationKey = fingerprint(config.AttestationPrivateKey)
	d.SourceCount = len(config.Sources)

	for i, src := range config.Sources {
		d.Sources = append(d.Sources, dumpSource{
			Index:            i,
			Type:             src.Type,
			ID:               b64(src.ID),
			RelyingPartyID:   src.RelyingParty.ID,
			RelyingPartyName: src.RelyingParty.Name,
			UserID:           b64(src.User.ID),
			UserName:         src.User.Name,
			UserDisplayName:  src.User.DisplayName,
			SignatureCounter: src.SignatureCounter,
			PrivateKey:       fingerprint(src.PrivateKey),
		})
	}
	d.Duplicates = findDuplicates(d.Sources)
	return d
}

// findDuplicates reports credential IDs, private keys and relying-party/user
// pairs that occur more than once - the signature of a corrupted vault.
func findDuplicates(sources []dumpSource) []string {
	var out []string
	count := func(label string, key func(dumpSource) string) {
		seen := map[string][]int{}
		for _, s := range sources {
			k := key(s)
			if k == "" {
				continue
			}
			seen[k] = append(seen[k], s.Index)
		}
		var keys []string
		for k, idx := range seen {
			if len(idx) > 1 {
				keys = append(keys, fmt.Sprintf("%s %s appears %d times at indices %v", label, k, len(idx), idx))
			}
		}
		sort.Strings(keys)
		out = append(out, keys...)
	}
	count("credential id", func(s dumpSource) string { return s.ID })
	count("private key", func(s dumpSource) string { return s.PrivateKey })
	count("relying party + user", func(s dumpSource) string { return s.RelyingPartyID + " / " + s.UserName })
	return out
}

func printSummary(d *dump) {
	fmt.Printf("file:          %s\n", d.File)
	fmt.Printf("format:        %s\n", d.Format)
	fmt.Printf("vault type:    %s\n", d.VaultType)
	fmt.Printf("last updated:  %s\n", d.LastUpdated)
	fmt.Printf("device key:    %s\n", d.EncryptionKey)
	fmt.Printf("attest cert:   %s\n", d.AttestationCert)
	fmt.Printf("attest key:    %s\n", d.AttestationKey)
	fmt.Printf("auth counter:  %d\n", d.AuthenticationCounter)
	fmt.Printf("pin enabled:   %v\n", d.PINEnabled)
	fmt.Printf("deleted srcs:  %d\n", len(d.DeletedSources))
	fmt.Printf("favicons:      %d %v\n", len(d.FaviconDomains), d.FaviconDomains)
	fmt.Printf("passkeys:      %d\n", d.SourceCount)
	for _, s := range d.Sources {
		fmt.Printf("  [%d] rp=%q rp_name=%q user=%q display=%q id=%s user_id=%s key=%s counter=%d\n",
			s.Index, s.RelyingPartyID, s.RelyingPartyName, s.UserName, s.UserDisplayName,
			s.ID, s.UserID, s.PrivateKey, s.SignatureCounter)
	}
	if len(d.Duplicates) > 0 {
		fmt.Printf("\nDUPLICATES DETECTED:\n")
		for _, dup := range d.Duplicates {
			fmt.Printf("  %s\n", dup)
		}
	}
}

func b64(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

// fingerprint returns a stable, diffable, non-secret representation of key
// material, or the real base64 value when -secrets was passed.
func fingerprint(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if showSecrets {
		return b64(data)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%s(%dB)", hex.EncodeToString(sum[:8]), len(data))
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
