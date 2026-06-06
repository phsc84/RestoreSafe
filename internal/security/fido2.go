//go:build windows

// FIDO2 hmac-secret authentication via the Windows WebAuthn API (webauthn.dll).
// No external tool or elevated privileges are required. The authenticator must
// support CTAP2 with the hmac-secret extension (all YubiKey 5 series devices do).
package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var fido2Debug = os.Getenv("RESTORESAFE_FIDO2_DEBUG") == "1"

func fido2Log(format string, args ...any) {
	if fido2Debug {
		fmt.Printf("[fido2-debug] "+format+"\n", args...)
	}
}

// ── Sentinel errors ───────────────────────────────────────────────────────────

var ErrYubiKeyNotConnected = errors.New("no FIDO2 authenticator detected")
var ErrYubiKeyRequired = errors.New("FIDO2 authenticator is required but none was detected. Remedy: Connect the YubiKey and retry.")

// ── Windows WebAuthn API bindings ─────────────────────────────────────────────

var (
	webauthnDLL                             = windows.NewLazySystemDLL("webauthn.dll")
	procWebAuthNGetApiVersionNumber         = webauthnDLL.NewProc("WebAuthNGetApiVersionNumber")
	procWebAuthNAuthenticatorMakeCredential = webauthnDLL.NewProc("WebAuthNAuthenticatorMakeCredential")
	procWebAuthNAuthenticatorGetAssertion   = webauthnDLL.NewProc("WebAuthNAuthenticatorGetAssertion")
	procWebAuthNFreeCredentialAttestation   = webauthnDLL.NewProc("WebAuthNFreeCredentialAttestation")
	procWebAuthNFreeAssertion               = webauthnDLL.NewProc("WebAuthNFreeAssertion")
	procGetConsoleWindow                    = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")
)

const (
	// webauthnMinAPIVersion is the minimum WebAuthn API version that supports
	// WEBAUTHN_AUTHENTICATOR_GET_ASSERTION_OPTIONS v6. That struct version adds
	// PRF/hmac-secret salt values.
	webauthnMinAPIVersion = 4

	webauthnRPEntityVersion   = 1
	webauthnUserEntityVersion = 1
	webauthnCredParamVersion  = 1
	webauthnClientDataVersion = 1
	webauthnCredentialVersion = 1

	webauthnMakeCredOptVersion     = 3
	webauthnGetAssertionOptVersion = 6

	webauthnAttachmentCrossPlatform = 2 // roaming authenticator (YubiKey)
	webauthnUVRequired              = 1 // require PIN / user verification
	webauthnAttestationNone         = 0
	webauthnHmacSecretValuesFlag    = 0x00100000 // use raw hmac-secret salt values

	webauthnAlgES256 = -7 // COSE ES256

	// fido2RPID is the WebAuthn Relying Party ID and fido2Origin the client-data
	// origin. RestoreSafe calls the native Windows WebAuthn API directly, so these
	// are never resolved or origin-validated (unlike in a browser) — they only act
	// as a stable namespace that scopes the FIDO2 credentials it creates.
	fido2RPID        = "restoresafe"
	fido2RPName      = "RestoreSafe"
	fido2Origin      = "restoresafe"
	fido2UserName    = "restoresafe-user"
	fido2DisplayName = "RestoreSafe User"
	fido2TimeoutMs   = 60000
	fido2SaltSize    = 32 // 256-bit hmac-secret salt → 32-byte output
	fido2CredIDMax   = 4096
	releasesURL      = "https://github.com/phsc84/RestoreSafe/releases"

	webAuthnCredTypePublicKey = "public-key"
	webAuthnHashAlgSHA256     = "SHA-256"
	webAuthnExtHmacSecret     = "hmac-secret"
)

// ── WebAuthn structs (64-bit Windows layout matching winwebauthn.h) ───────────
//
// Each struct is annotated with expected size and field offsets. Mismatches here
// will cause the WebAuthn API to reject or misinterpret calls.

// webauthnRPEntity matches WEBAUTHN_RP_ENTITY_INFORMATION v1. Size: 32 bytes.
type webauthnRPEntity struct {
	dwVersion uint32  // 0
	_         uint32  // 4  padding
	pwszId    *uint16 // 8
	pwszName  *uint16 // 16
	pwszIcon  *uint16 // 24
}

// webauthnUserEntity matches WEBAUTHN_USER_ENTITY_INFORMATION v1. Size: 40 bytes.
type webauthnUserEntity struct {
	dwVersion       uint32  // 0
	cbId            uint32  // 4
	pbId            *byte   // 8
	pwszName        *uint16 // 16
	pwszIcon        *uint16 // 24
	pwszDisplayName *uint16 // 32
}

// webauthnCoseCredParam matches WEBAUTHN_COSE_CREDENTIAL_PARAMETER v1. Size: 24 bytes.
type webauthnCoseCredParam struct {
	dwVersion          uint32  // 0
	_                  uint32  // 4  padding
	pwszCredentialType *uint16 // 8
	lAlg               int32   // 16
	_                  int32   // 20 padding (struct size must be multiple of 8)
}

// webauthnCoseCredParams matches WEBAUTHN_COSE_CREDENTIAL_PARAMETERS. Size: 16 bytes.
type webauthnCoseCredParams struct {
	cCredentialParameters uint32                 // 0
	_                     uint32                 // 4  padding
	pCredentialParameters *webauthnCoseCredParam // 8
}

// webauthnClientData matches WEBAUTHN_CLIENT_DATA v1. Size: 24 bytes.
type webauthnClientData struct {
	dwVersion        uint32  // 0
	cbClientDataJSON uint32  // 4
	pbClientDataJSON *byte   // 8
	pwszHashAlgId    *uint16 // 16
}

// webauthnCredential matches WEBAUTHN_CREDENTIAL v1. Size: 24 bytes.
type webauthnCredential struct {
	dwVersion          uint32  // 0
	cbId               uint32  // 4
	pbId               *byte   // 8
	pwszCredentialType *uint16 // 16
}

// webauthnCredentials matches WEBAUTHN_CREDENTIALS. Size: 16 bytes.
type webauthnCredentials struct {
	cCredentials uint32              // 0
	_            uint32              // 4  padding
	pCredentials *webauthnCredential // 8
}

// webauthnExtension matches WEBAUTHN_EXTENSION. Size: 24 bytes.
type webauthnExtension struct {
	pwszExtensionIdentifier *uint16        // 0
	cbExtension             uint32         // 8
	_                       uint32         // 12 padding
	pvExtension             unsafe.Pointer // 16
}

// webauthnExtensions matches WEBAUTHN_EXTENSIONS. Size: 16 bytes.
type webauthnExtensions struct {
	cExtensions uint32             // 0
	_           uint32             // 4  padding
	pExtensions *webauthnExtension // 8
}

// webauthnMakeCredentialOptions matches WEBAUTHN_AUTHENTICATOR_MAKE_CREDENTIAL_OPTIONS v3.
// Size: 96 bytes.
type webauthnMakeCredentialOptions struct {
	dwVersion                         uint32              // 0
	dwTimeoutMilliseconds             uint32              // 4
	CredentialList                    webauthnCredentials // 8   exclude list (empty)
	Extensions                        webauthnExtensions  // 24
	dwAuthenticatorAttachment         uint32              // 40
	bRequireResidentKey               int32               // 44  BOOL
	dwUserVerificationRequirement     uint32              // 48
	dwAttestationConveyancePreference uint32              // 52
	dwFlags                           uint32              // 56
	_                                 uint32              // 60  padding
	pCancellationId                   *windows.GUID       // 64
	pExcludeCredentialList            unsafe.Pointer      // 72
	dwEnterpriseAttestation           uint32              // 80
	dwLargeBlobSupport                uint32              // 84
	bPreferResidentKey                int32               // 88  BOOL
	_                                 uint32              // 92  padding
}

// webauthnCredAttestation matches WEBAUTHN_CREDENTIAL_ATTESTATION through pbCredentialId.
// Only the fields up to pbCredentialId (offset 88) are read.
type webauthnCredAttestation struct {
	dwVersion               uint32         // 0
	_                       uint32         // 4
	pwszFormatIdentifier    *uint16        // 8
	cbAuthenticatorData     uint32         // 16
	_                       uint32         // 20
	pbAuthenticatorData     *byte          // 24
	cbAttestation           uint32         // 32
	_                       uint32         // 36
	pbAttestation           *byte          // 40
	dwAttestationDecodeType uint32         // 48
	_                       uint32         // 52
	pvAttestationDecode     unsafe.Pointer // 56
	cbAttestationObject     uint32         // 64
	_                       uint32         // 68
	pbAttestationObject     *byte          // 72
	cbCredentialId          uint32         // 80
	_                       uint32         // 84
	pbCredentialId          *byte          // 88
}

// webauthnHmacSecretSalt matches WEBAUTHN_HMAC_SECRET_SALT. Size: 32 bytes.
type webauthnHmacSecretSalt struct {
	cbFirst  uint32 // 0
	_        uint32 // 4  padding
	pbFirst  *byte  // 8
	cbSecond uint32 // 16
	_        uint32 // 20 padding
	pbSecond *byte  // 24
}

// webauthnHmacSecretSaltValues matches WEBAUTHN_HMAC_SECRET_SALT_VALUES. Size: 24 bytes.
type webauthnHmacSecretSaltValues struct {
	pGlobalHmacSalt             *webauthnHmacSecretSalt // 0
	cCredWithHmacSecretSaltList uint32                  // 8
	_                           uint32                  // 12 padding
	pCredWithHmacSecretSaltList unsafe.Pointer          // 16
}

// webauthnGetAssertionOptions matches WEBAUTHN_AUTHENTICATOR_GET_ASSERTION_OPTIONS v6.
// Size: 120 bytes.
type webauthnGetAssertionOptions struct {
	dwVersion                     uint32                        // 0
	dwTimeoutMilliseconds         uint32                        // 4
	CredentialList                webauthnCredentials           // 8   allowed credentials
	Extensions                    webauthnExtensions            // 24
	dwAuthenticatorAttachment     uint32                        // 40
	dwUserVerificationRequirement uint32                        // 44
	dwFlags                       uint32                        // 48
	_                             uint32                        // 52  padding
	pwszU2fAppId                  *uint16                       // 56
	pbU2fAppId                    *int32                        // 64  *BOOL
	pCancellationId               *windows.GUID                 // 72
	pAllowCredentialList          unsafe.Pointer                // 80
	dwCredLargeBlobOperation      uint32                        // 88
	cbCredLargeBlob               uint32                        // 92
	pbCredLargeBlob               *byte                         // 96
	pHmacSecretSaltValues         *webauthnHmacSecretSaltValues // 104
	bBrowserInPrivateMode         int32                         // 112 BOOL
	_                             uint32                        // 116 padding
}

// webauthnAssertion matches WEBAUTHN_ASSERTION v3. Size: 120 bytes.
type webauthnAssertion struct {
	dwVersion             uint32                  // 0
	cbAuthenticatorData   uint32                  // 4
	pbAuthenticatorData   *byte                   // 8
	cbSignature           uint32                  // 16
	_                     uint32                  // 20  padding
	pbSignature           *byte                   // 24
	Credential            webauthnCredential      // 32  (24 bytes)
	cbUserId              uint32                  // 56
	_                     uint32                  // 60  padding
	pbUserId              *byte                   // 64
	Extensions            webauthnExtensions      // 72  (16 bytes)
	cbCredLargeBlob       uint32                  // 88
	_                     uint32                  // 92  padding
	pbCredLargeBlob       *byte                   // 96
	dwCredLargeBlobStatus uint32                  // 104
	_                     uint32                  // 108 padding
	pHmacSecret           *webauthnHmacSecretSalt // 112
}

// ── Challenge file format ─────────────────────────────────────────────────────

// ChallengeData is the JSON content of a FIDO2 .challenge file.
// Version 1.
//
// Sum is a SHA-256 checksum over the other fields. It detects accidental
// corruption or truncation of the .challenge file and lets restore report a
// clear "corrupted challenge file" error instead of a misleading "wrong
// password" failure when, for example, a bit-flip leaves Salt the right length
// but with the wrong bytes. It is NOT a defense against deliberate tampering
// (an attacker can recompute it): the cryptographic binding between a backup and
// its credential ID + salt is provided by Argon2id key derivation and AES-GCM
// authentication — any change to those values yields the wrong key and fails
// decryption.
type ChallengeData struct {
	Version    int    `json:"v"`
	NoPassword bool   `json:"nopw"`
	CredID     string `json:"cred_id"`       // base64 std-encoding of FIDO2 credential ID
	Salt       string `json:"salt"`          // base64 std-encoding of 32-byte hmac-secret salt
	Sum        string `json:"sum,omitempty"` // base64 std-encoding of SHA-256 over the fields above
}

// challengeChecksum returns the base64 SHA-256 checksum over the integrity-
// relevant challenge fields (everything except Sum itself).
func challengeChecksum(cd ChallengeData) string {
	canonical := fmt.Sprintf("v=%d;nopw=%t;cred_id=%s;salt=%s", cd.Version, cd.NoPassword, cd.CredID, cd.Salt)
	sum := sha256.Sum256([]byte(canonical))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// ParseChallengeFile reads and parses the JSON from a .challenge file path.
func ParseChallengeFile(path string) (ChallengeData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ChallengeData{}, err
	}
	return parseChallengeData(strings.TrimSpace(string(data)))
}

// ValidateChallengeJSON reports whether s is a well-formed FIDO2 challenge JSON string.
func ValidateChallengeJSON(s string) error {
	_, err := parseChallengeData(s)
	return err
}

func parseChallengeData(s string) (ChallengeData, error) {
	if s == "" {
		return ChallengeData{}, fmt.Errorf("challenge data is empty")
	}
	var cd ChallengeData
	if err := json.Unmarshal([]byte(s), &cd); err != nil {
		return ChallengeData{}, fmt.Errorf("challenge data is not valid JSON: %w", err)
	}
	credID, err := base64.StdEncoding.DecodeString(cd.CredID)
	if err != nil || len(credID) == 0 || len(credID) > fido2CredIDMax {
		return ChallengeData{}, fmt.Errorf("challenge credential ID is missing or invalid")
	}
	salt, err := base64.StdEncoding.DecodeString(cd.Salt)
	if err != nil || len(salt) != fido2SaltSize {
		return ChallengeData{}, fmt.Errorf("challenge salt is missing or wrong length (want %d bytes)", fido2SaltSize)
	}
	// Verify the integrity checksum when present. Files written by this version
	// always include it; tolerate its absence so externally constructed v2
	// challenges remain valid.
	if cd.Sum != "" && cd.Sum != challengeChecksum(cd) {
		return ChallengeData{}, fmt.Errorf("challenge file is corrupted (integrity checksum mismatch)")
	}
	return cd, nil
}

// ── Injectable vars (overridden in tests) ─────────────────────────────────────

// fido2MakeCredFn creates a new FIDO2 credential with the hmac-secret extension
// enabled and returns the raw credential ID.
var fido2MakeCredFn = realMakeCredential

// fido2GetHmacFn returns the 32-byte hmac-secret output for the given credential
// ID and salt.
var fido2GetHmacFn = realGetHmacSecret

// fido2RuntimeReady returns nil when the platform and authenticator are ready.
var fido2RuntimeReady = func() error {
	if err := checkWebAuthnAPIVersion(); err != nil {
		return err
	}
	if !yubiKeyVIDVisible() {
		return ErrYubiKeyNotConnected
	}
	return nil
}

// ── Public API ────────────────────────────────────────────────────────────────

// CheckYubiKeyAvailability returns nil when the Windows WebAuthn API is present
// and new enough to support PRF/hmac-secret salt values in GetAssertion options.
func CheckYubiKeyAvailability() error {
	return checkWebAuthnAPIVersion()
}

// CheckYubiKeyConnected returns nil when a Yubico FIDO2 authenticator is
// detected and the WebAuthn platform is ready.
func CheckYubiKeyConnected() error {
	return fido2RuntimeReady()
}

// YubiKeyHIDDevicePaths returns HID device interface paths for connected
// Yubico devices. It is used by diagnostics that need more detail than the
// boolean runtime readiness check.
func YubiKeyHIDDevicePaths() ([]string, error) {
	return hidDevicePathsForVID(yubicoVID)
}

// CombineWithPassword creates a new FIDO2 credential, derives a 32-byte
// hmac-secret from it using a random salt, and appends it to password.
// noPassword must be true when the backup uses no password (YubiKey-only mode)
// so that restore can skip the password prompt.
// Returns the combined key and a JSON challenge string to store alongside the backup.
func CombineWithPassword(password []byte, noPassword bool) (combined []byte, challengeJSON string, err error) {
	credID, err := fido2MakeCredFn()
	if err != nil {
		return nil, "", fmt.Errorf("FIDO2 credential creation failed: %w", err)
	}

	salt := make([]byte, fido2SaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, "", fmt.Errorf("failed to generate hmac-secret salt: %w", err)
	}

	secret, err := fido2GetHmacFn(credID, salt)
	if err != nil {
		return nil, "", fmt.Errorf("FIDO2 hmac-secret failed: %w", err)
	}
	if err := validateFIDO2Secret(secret); err != nil {
		ZeroBytes(secret)
		return nil, "", err
	}
	defer ZeroBytes(secret)

	cd := ChallengeData{
		Version:    1,
		NoPassword: noPassword,
		CredID:     base64.StdEncoding.EncodeToString(credID),
		Salt:       base64.StdEncoding.EncodeToString(salt),
	}
	cd.Sum = challengeChecksum(cd)
	jsonBytes, err := json.Marshal(cd)
	if err != nil {
		return nil, "", fmt.Errorf("failed to encode challenge data: %w", err)
	}

	return CombinePasswordWithSecret(password, secret), string(jsonBytes), nil
}

// DeriveFIDO2SecretForRestore reproduces the 32-byte FIDO2 hmac-secret for a
// stored challenge by performing a single GetAssertion (one user touch). The
// secret is constant for a given challenge, so a caller that retries a password
// can derive it once and reuse it across attempts via CombinePasswordWithSecret
// instead of prompting for another touch each time. The caller owns the returned
// slice and must zero it when done.
func DeriveFIDO2SecretForRestore(challengeJSON string) ([]byte, error) {
	cd, err := parseChallengeData(challengeJSON)
	if err != nil {
		return nil, fmt.Errorf("invalid FIDO2 challenge: %w. Remedy: Ensure the .challenge file is unchanged and belongs to the same backup run as the .enc files.", err)
	}

	credID, _ := base64.StdEncoding.DecodeString(cd.CredID)
	salt, _ := base64.StdEncoding.DecodeString(cd.Salt)

	secret, err := fido2GetHmacFn(credID, salt)
	if err != nil {
		return nil, fmt.Errorf("FIDO2 hmac-secret failed: %w", err)
	}
	if err := validateFIDO2Secret(secret); err != nil {
		ZeroBytes(secret)
		return nil, err
	}
	return secret, nil
}

// CombinePasswordWithSecret returns password followed by secret. This is the key
// material input shared by backup and restore whenever a YubiKey factor is used.
func CombinePasswordWithSecret(password, secret []byte) []byte {
	combined := make([]byte, len(password)+len(secret))
	copy(combined, password)
	copy(combined[len(password):], secret)
	return combined
}

// ── Internal WebAuthn implementation ─────────────────────────────────────────

func validateFIDO2Secret(secret []byte) error {
	if len(secret) != fido2SaltSize {
		return fmt.Errorf("FIDO2 hmac-secret returned %d bytes, want %d", len(secret), fido2SaltSize)
	}
	return nil
}

func checkWebAuthnAPIVersion() error {
	if err := webauthnDLL.Load(); err != nil {
		return fmt.Errorf("webauthn.dll not available: %w. Remedy: Windows 11 version 22H2 or later is required.", err)
	}
	ver, _, _ := procWebAuthNGetApiVersionNumber.Call()
	fido2Log("webauthn.dll API version: %d (minimum required: %d)", ver, webauthnMinAPIVersion)
	if ver < webauthnMinAPIVersion {
		return fmt.Errorf("webauthn.dll API version %d is too old (need %d+). Remedy: Windows 11 version 22H2 or later is required.", ver, webauthnMinAPIVersion)
	}
	return nil
}

func consoleWindow() uintptr {
	hwnd, _, _ := procGetConsoleWindow.Call()
	return hwnd
}

// buildClientData constructs the clientDataJSON bytes and returns them along
// with a pointer suitable for passing to the WebAuthn API.
func buildClientData(opType string) ([]byte, error) {
	challenge := make([]byte, 16)
	if _, err := rand.Read(challenge); err != nil {
		return nil, err
	}
	b64 := base64.RawURLEncoding.EncodeToString(challenge)
	j := fmt.Sprintf(`{"type":%q,"challenge":%q,"origin":%q,"crossOrigin":false}`, opType, b64, fido2Origin)
	return []byte(j), nil
}

func webauthnErrorString(hr uintptr) string {
	return fmt.Sprintf("HRESULT 0x%08X", hr)
}

// realMakeCredential calls WebAuthNAuthenticatorMakeCredential to create a new
// non-resident FIDO2 credential with the hmac-secret extension enabled.
// The returned credential ID is an opaque blob that the authenticator uses to
// derive the hmac-secret on subsequent GetAssertion calls.
func realMakeCredential() (credID []byte, err error) {
	clientDataBytes, err := buildClientData("webauthn.create")
	if err != nil {
		return nil, fmt.Errorf("failed to build client data: %w", err)
	}

	rpIdW := windows.StringToUTF16Ptr(fido2RPID)
	rpNameW := windows.StringToUTF16Ptr(fido2RPName)
	rp := webauthnRPEntity{
		dwVersion: webauthnRPEntityVersion,
		pwszId:    rpIdW,
		pwszName:  rpNameW,
	}

	userID := make([]byte, 8)
	if _, err := rand.Read(userID); err != nil {
		return nil, fmt.Errorf("failed to generate user ID: %w", err)
	}
	userNameW := windows.StringToUTF16Ptr(fido2UserName)
	userDisplayW := windows.StringToUTF16Ptr(fido2DisplayName)
	user := webauthnUserEntity{
		dwVersion:       webauthnUserEntityVersion,
		cbId:            uint32(len(userID)),
		pbId:            &userID[0],
		pwszName:        userNameW,
		pwszDisplayName: userDisplayW,
	}

	credTypeW := windows.StringToUTF16Ptr(webAuthnCredTypePublicKey)
	credParam := webauthnCoseCredParam{
		dwVersion:          webauthnCredParamVersion,
		pwszCredentialType: credTypeW,
		lAlg:               webauthnAlgES256,
	}
	credParams := webauthnCoseCredParams{
		cCredentialParameters: 1,
		pCredentialParameters: &credParam,
	}

	hashAlgW := windows.StringToUTF16Ptr(webAuthnHashAlgSHA256)
	cd := webauthnClientData{
		dwVersion:        webauthnClientDataVersion,
		cbClientDataJSON: uint32(len(clientDataBytes)),
		pbClientDataJSON: &clientDataBytes[0],
		pwszHashAlgId:    hashAlgW,
	}

	// Enable hmac-secret extension: pvExtension points to BOOL=TRUE.
	hmacSecretEnabled := int32(1)
	extIdW := windows.StringToUTF16Ptr(webAuthnExtHmacSecret)
	ext := webauthnExtension{
		pwszExtensionIdentifier: extIdW,
		cbExtension:             4, // sizeof(BOOL)
		pvExtension:             unsafe.Pointer(&hmacSecretEnabled),
	}
	exts := webauthnExtensions{
		cExtensions: 1,
		pExtensions: &ext,
	}

	opts := webauthnMakeCredentialOptions{
		dwVersion:                         webauthnMakeCredOptVersion,
		dwTimeoutMilliseconds:             fido2TimeoutMs,
		Extensions:                        exts,
		dwAuthenticatorAttachment:         webauthnAttachmentCrossPlatform,
		dwUserVerificationRequirement:     webauthnUVRequired,
		dwAttestationConveyancePreference: webauthnAttestationNone,
	}

	var attestation *webauthnCredAttestation
	hr, _, _ := procWebAuthNAuthenticatorMakeCredential.Call(
		consoleWindow(),
		uintptr(unsafe.Pointer(&rp)),
		uintptr(unsafe.Pointer(&user)),
		uintptr(unsafe.Pointer(&credParams)),
		uintptr(unsafe.Pointer(&cd)),
		uintptr(unsafe.Pointer(&opts)),
		uintptr(unsafe.Pointer(&attestation)),
	)
	if hr != 0 {
		return nil, fmt.Errorf("WebAuthNAuthenticatorMakeCredential: %s. Remedy: Ensure the YubiKey is connected, supports FIDO2 hmac-secret, and has a FIDO2 PIN configured.", webauthnErrorString(hr))
	}
	if attestation == nil || attestation.cbCredentialId == 0 {
		return nil, fmt.Errorf("WebAuthNAuthenticatorMakeCredential returned no credential ID")
	}
	defer procWebAuthNFreeCredentialAttestation.Call(uintptr(unsafe.Pointer(attestation)))

	credID = make([]byte, attestation.cbCredentialId)
	copy(credID, unsafe.Slice(attestation.pbCredentialId, attestation.cbCredentialId))
	fido2Log("credential ID length: %d bytes", len(credID))
	return credID, nil
}

// realGetHmacSecret calls WebAuthNAuthenticatorGetAssertion with the hmac-secret
// extension to derive a 32-byte secret from the given credential ID and salt.
func realGetHmacSecret(credID, salt []byte) ([]byte, error) {
	if len(salt) != fido2SaltSize {
		return nil, fmt.Errorf("hmac-secret salt must be %d bytes, got %d", fido2SaltSize, len(salt))
	}

	clientDataBytes, err := buildClientData("webauthn.get")
	if err != nil {
		return nil, fmt.Errorf("failed to build client data: %w", err)
	}

	rpIdW := windows.StringToUTF16Ptr(fido2RPID)

	hashAlgW := windows.StringToUTF16Ptr(webAuthnHashAlgSHA256)
	cd := webauthnClientData{
		dwVersion:        webauthnClientDataVersion,
		cbClientDataJSON: uint32(len(clientDataBytes)),
		pbClientDataJSON: &clientDataBytes[0],
		pwszHashAlgId:    hashAlgW,
	}

	credTypeW := windows.StringToUTF16Ptr(webAuthnCredTypePublicKey)
	allowedCred := webauthnCredential{
		dwVersion:          webauthnCredentialVersion,
		cbId:               uint32(len(credID)),
		pbId:               &credID[0],
		pwszCredentialType: credTypeW,
	}
	allowedList := webauthnCredentials{
		cCredentials: 1,
		pCredentials: &allowedCred,
	}

	hmacSalt := webauthnHmacSecretSalt{
		cbFirst: uint32(len(salt)),
		pbFirst: &salt[0],
	}
	saltValues := webauthnHmacSecretSaltValues{
		pGlobalHmacSalt: &hmacSalt,
	}

	opts := webauthnGetAssertionOptions{
		dwVersion:                     webauthnGetAssertionOptVersion,
		dwTimeoutMilliseconds:         fido2TimeoutMs,
		CredentialList:                allowedList,
		dwAuthenticatorAttachment:     webauthnAttachmentCrossPlatform,
		dwUserVerificationRequirement: webauthnUVRequired,
		dwFlags:                       webauthnHmacSecretValuesFlag,
		pHmacSecretSaltValues:         &saltValues,
	}

	var assertion *webauthnAssertion
	hr, _, _ := procWebAuthNAuthenticatorGetAssertion.Call(
		consoleWindow(),
		uintptr(unsafe.Pointer(rpIdW)),
		uintptr(unsafe.Pointer(&cd)),
		uintptr(unsafe.Pointer(&opts)),
		uintptr(unsafe.Pointer(&assertion)),
	)
	if hr != 0 {
		return nil, fmt.Errorf("WebAuthNAuthenticatorGetAssertion: %s. Remedy: Ensure the correct YubiKey is connected (the one used during backup).", webauthnErrorString(hr))
	}
	if assertion == nil || assertion.pHmacSecret == nil {
		return nil, fmt.Errorf("WebAuthNAuthenticatorGetAssertion returned no hmac-secret output")
	}
	defer procWebAuthNFreeAssertion.Call(uintptr(unsafe.Pointer(assertion)))
	hmacSecret := assertion.pHmacSecret
	if hmacSecret.cbFirst == 0 {
		return nil, fmt.Errorf("WebAuthNAuthenticatorGetAssertion returned empty hmac-secret first output")
	}

	out := make([]byte, hmacSecret.cbFirst)
	copy(out, unsafe.Slice(hmacSecret.pbFirst, hmacSecret.cbFirst))
	fido2Log("hmac-secret output length: %d bytes", len(out))
	return out, nil
}

// ── HID presence check (SetupDi) ─────────────────────────────────────────────

var (
	setupapiDLL = windows.NewLazySystemDLL("setupapi.dll")

	procSetupDiGetClassDevsW             = setupapiDLL.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInterfaces      = setupapiDLL.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetDeviceInterfaceDetailW = setupapiDLL.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDiDestroyDeviceInfoList     = setupapiDLL.NewProc("SetupDiDestroyDeviceInfoList")
)

var guidDevInterfaceHID = windows.GUID{
	Data1: 0x4D1E55B2,
	Data2: 0xF16F,
	Data3: 0x11CF,
	Data4: [8]byte{0x88, 0xCB, 0x00, 0x11, 0x11, 0x00, 0x00, 0x30},
}

const (
	digcfPresent         = 0x00000002
	digcfDeviceInterface = 0x00000010
	yubicoVID            = 0x1050
)

type spDeviceInterfaceData struct {
	Size               uint32
	InterfaceClassGuid windows.GUID
	Flags              uint32
	Reserved           uintptr
}

// yubiKeyVIDVisible returns true if any HID device path contains the Yubico VID.
// Used as a lightweight presence check that does not open any device handle.
func yubiKeyVIDVisible() bool {
	paths, err := YubiKeyHIDDevicePaths()
	return err == nil && len(paths) > 0
}

func hidDevicePathsForVID(vendorID uint16) ([]string, error) {
	devInfo, _, _ := procSetupDiGetClassDevsW.Call(
		uintptr(unsafe.Pointer(&guidDevInterfaceHID)),
		0, 0,
		uintptr(digcfPresent|digcfDeviceInterface),
	)
	if devInfo == ^uintptr(0) {
		return nil, fmt.Errorf("SetupDiGetClassDevsW failed")
	}
	defer procSetupDiDestroyDeviceInfoList.Call(devInfo)

	vidToken := fmt.Sprintf("VID_%04X", vendorID)
	var paths []string
	for idx := uint32(0); idx < 1000; idx++ {
		var ifData spDeviceInterfaceData
		ifData.Size = uint32(unsafe.Sizeof(ifData))
		ret, _, _ := procSetupDiEnumDeviceInterfaces.Call(
			devInfo, 0,
			uintptr(unsafe.Pointer(&guidDevInterfaceHID)),
			uintptr(idx),
			uintptr(unsafe.Pointer(&ifData)),
		)
		if ret == 0 {
			break
		}
		path, err := deviceInterfacePath(devInfo, &ifData)
		if err == nil && strings.Contains(strings.ToUpper(path), vidToken) {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func deviceInterfacePath(devInfo uintptr, ifData *spDeviceInterfaceData) (string, error) {
	var requiredSize uint32
	procSetupDiGetDeviceInterfaceDetailW.Call(
		devInfo,
		uintptr(unsafe.Pointer(ifData)),
		0, 0,
		uintptr(unsafe.Pointer(&requiredSize)),
		0,
	)
	if requiredSize == 0 {
		return "", fmt.Errorf("SetupDiGetDeviceInterfaceDetail: zero size")
	}
	buf := make([]byte, requiredSize)
	if len(buf) < 4 {
		return "", fmt.Errorf("SetupDiGetDeviceInterfaceDetail: buffer too small")
	}
	// cbSize must be 8 on 64-bit Windows (pack(1) in SDK header notwithstanding).
	*(*uint32)(unsafe.Pointer(&buf[0])) = 8
	ret, _, err := procSetupDiGetDeviceInterfaceDetailW.Call(
		devInfo,
		uintptr(unsafe.Pointer(ifData)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(requiredSize),
		uintptr(unsafe.Pointer(&requiredSize)),
		0,
	)
	if ret == 0 {
		return "", fmt.Errorf("SetupDiGetDeviceInterfaceDetail: %w", err)
	}
	pathUTF16 := unsafe.Slice((*uint16)(unsafe.Pointer(&buf[4])), (len(buf)-4)/2)
	return windows.UTF16ToString(pathUTF16), nil
}
