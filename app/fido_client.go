package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"

	"github.com/bulwarkid/virtual-fido/cose"
	vfido_crypto "github.com/bulwarkid/virtual-fido/crypto"
	"github.com/bulwarkid/virtual-fido/identities"
	"github.com/bulwarkid/virtual-fido/webauthn"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type FIDOClientDelegate interface {
	FIDOUpdated()
}

type FIDOClient struct {
	delegate FIDOClientDelegate

	vault                 *identities.IdentityVault
	certificateAuthority  *x509.Certificate
	certPrivateKey        *ecdsa.PrivateKey
	encryptionKey         []byte
	authenticationCounter uint32

	pinHash         []byte
	pinRetries      int32
	pinKeyAgreement *vfido_crypto.ECDHKey
	pinToken        []byte
}

func newFIDOClient(delegate FIDOClientDelegate) *FIDOClient {
	return &FIDOClient{
		delegate:        delegate,
		pinToken:        randomBytes(16),
		pinRetries:      8,
		pinHash:         nil,
		pinKeyAgreement: vfido_crypto.GenerateECDHKey(),
		vault:           identities.NewIdentityVault(),
	}
}

func (client *FIDOClient) configureNewClient() {
	authority := &x509.Certificate{
		SerialNumber: big.NewInt(0),
		Subject: pkix.Name{
			Organization: []string{"Bulwark Passkey"},
			Country:      []string{"US"},
		},
		NotBefore:             now(),
		NotAfter:              now().AddDate(10, 0, 0),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	checkErr(err, "Could not generate attestation CA private key")
	authorityCertBytes, err := x509.CreateCertificate(rand.Reader, authority, authority, &privateKey.PublicKey, privateKey)
	checkErr(err, "Could not generate attestation CA cert bytes")
	certificateAuthority, err := x509.ParseCertificate(authorityCertBytes)
	checkErr(err, "Could not parse cert authority")
	encryptionKey := randomBytes(32)
	client.authenticationCounter = 0
	client.certificateAuthority = certificateAuthority
	client.certPrivateKey = privateKey
	client.encryptionKey = encryptionKey
	client.vault = identities.NewIdentityVault()
}

func (client *FIDOClient) loadConfig(config *identities.FIDODeviceConfig) {
	cert, err := x509.ParseCertificate(config.AttestationCertificate)
	checkErr(err, "Could not parse x509 cert")
	privateKey, err := x509.ParseECPrivateKey(config.AttestationPrivateKey)
	checkErr(err, "Could not parse private key")
	client.authenticationCounter = config.AuthenticationCounter
	client.certificateAuthority = cert
	client.certPrivateKey = privateKey
	client.encryptionKey = config.EncryptionKey
	client.pinHash = config.PINHash
	client.vault = identities.NewIdentityVault()
	client.vault.Import(config.Sources)
}

func (client *FIDOClient) exportConfig() *identities.FIDODeviceConfig {
	privateKey, err := x509.MarshalECPrivateKey(client.certPrivateKey)
	checkErr(err, "Could not encode private key")
	config := identities.FIDODeviceConfig{
		AuthenticationCounter:  client.authenticationCounter,
		AttestationCertificate: client.certificateAuthority.Raw,
		AttestationPrivateKey:  privateKey,
		EncryptionKey:          client.encryptionKey,
		PINEnabled:             client.SupportsPIN(),
		PINHash:                client.pinHash,
		Sources:                client.vault.Export(),
	}
	return &config
}

// SupportsResidentKey reports discoverable credential support; every passkey in
// the vault is stored on the device, so resident keys are always available.
func (client *FIDOClient) SupportsResidentKey() bool {
	return true
}

func (client *FIDOClient) NewCredentialSource(
	pubKeyCredParams []webauthn.PublicKeyCredentialParams,
	excludeList []webauthn.PublicKeyCredentialDescriptor,
	relyingParty *webauthn.PublicKeyCredentialRPEntity,
	user *webauthn.PublicKeyCrendentialUserEntity) *identities.CredentialSource {
	// The vault only creates ES256 credentials, so refuse relying parties that
	// ask for anything else instead of handing back a key they cannot verify.
	supported := false
	for _, param := range pubKeyCredParams {
		if param.Type == "public-key" && param.Algorithm == cose.COSE_ALGORITHM_ID_ES256 {
			supported = true
			break
		}
	}
	if !supported {
		return nil
	}
	id := client.vault.NewIdentity(relyingParty, user)
	client.delegate.FIDOUpdated()
	return id
}

func (client *FIDOClient) GetAssertionSource(relyingPartyID string, allowList []webauthn.PublicKeyCredentialDescriptor) *identities.CredentialSource {
	sources := client.vault.GetMatchingCredentialSources(relyingPartyID, allowList)
	if len(sources) == 0 {
		return nil
	}

	// TODO: Allow user to choose credential source
	credentialSource := sources[0]
	credentialSource.SignatureCounter++
	client.delegate.FIDOUpdated()
	return credentialSource
}

func (client *FIDOClient) SealingEncryptionKey() []byte {
	return client.encryptionKey
}

func (client *FIDOClient) NewPrivateKey() *ecdsa.PrivateKey {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	checkErr(err, "Could not generate private key")
	return privateKey
}

func (client *FIDOClient) NewAuthenticationCounterId() uint32 {
	num := client.authenticationCounter
	client.authenticationCounter++
	client.delegate.FIDOUpdated()
	return num
}

func (client *FIDOClient) CreateAttestationCertificiate(privateKey *cose.SupportedCOSEPrivateKey) []byte {
	// TODO: Fill in fields like SerialNumber and SubjectKeyIdentifier
	templateCert := &x509.Certificate{
		SerialNumber: big.NewInt(0),
		Subject: pkix.Name{
			Organization:       []string{"Self-Signed Virtual FIDO"},
			Country:            []string{"US"},
			CommonName:         "Self-Signed Virtual FIDO",
			OrganizationalUnit: []string{"Authenticator Attestation"},
		},
		NotBefore:             now(),
		NotAfter:              now().AddDate(10, 0, 0),
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature,
		IsCA:                  false,
		BasicConstraintsValid: true,
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, templateCert, client.certificateAuthority, cosePublicKey(privateKey), client.certPrivateKey)
	checkErr(err, "Could not generate attestation certificate")
	return certBytes
}

// cosePublicKey unwraps the COSE key container virtual-fido now passes around
// into the concrete public key type crypto/x509 expects.
func cosePublicKey(privateKey *cose.SupportedCOSEPrivateKey) crypto.PublicKey {
	publicKey := privateKey.Public()
	switch {
	case publicKey.ECDSA != nil:
		return publicKey.ECDSA
	case publicKey.Ed25519 != nil:
		return *publicKey.Ed25519
	case publicKey.RSA != nil:
		return publicKey.RSA
	default:
		fatalf("Unsupported COSE key type for attestation certificate")
		return nil
	}
}

func (client *FIDOClient) getApproval(action, relyingParty, userName string) bool {
	runtime.WindowShow(app.ctx)
	approved := approveClientAction(action, relyingParty, userName)
	runtime.WindowMinimise(app.ctx)
	return approved
}

func (client *FIDOClient) ApproveAccountCreation(relyingParty string) bool {
	return client.getApproval("fido_make_credential", relyingParty, "")
}

func (client *FIDOClient) ApproveAccountLogin(credentialSource *identities.CredentialSource) bool {
	relyingParty := firstNonEmpty(credentialSource.RelyingParty.Name, credentialSource.RelyingParty.ID)
	userName := firstNonEmpty(credentialSource.User.DisplayName, credentialSource.User.Name)
	return client.getApproval("fido_get_assertion", relyingParty, userName)
}

func (client *FIDOClient) ApproveU2FRegistration(keyHandle *webauthn.KeyHandle) bool {
	return client.getApproval("u2f_register", "", "")
}

func (client *FIDOClient) ApproveU2FAuthentication(keyHandle *webauthn.KeyHandle) bool {
	return client.getApproval("u2f_authenticate", "", "")
}

func (client *FIDOClient) SupportsPIN() bool {
	return true
}

func (client *FIDOClient) PINHash() []byte {
	return client.pinHash
}

func (client *FIDOClient) SetPINHash(pin []byte) {
	client.pinHash = pin
	client.delegate.FIDOUpdated()
}

func (client *FIDOClient) PINRetries() int32 {
	return client.pinRetries
}

func (client *FIDOClient) SetPINRetries(retries int32) {
	// No need to notify updates as retries aren't saved between sessions
	client.pinRetries = retries
}

func (client *FIDOClient) PINKeyAgreement() *vfido_crypto.ECDHKey {
	return client.pinKeyAgreement
}

func (client *FIDOClient) PINToken() []byte {
	return client.pinToken
}
