//go:build windows

package yubikey

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

//lint:file-ignore ST1003 names follow winwebauthn.h, so they match the Windows API documentation

// ── Windows WebAuthn API bindings ─────────────────────────────────────────────

var (
	webauthnDLL                             = windows.NewLazySystemDLL("webauthn.dll")
	procWebAuthNGetApiVersionNumber         = webauthnDLL.NewProc("WebAuthNGetApiVersionNumber")
	procWebAuthNAuthenticatorMakeCredential = webauthnDLL.NewProc("WebAuthNAuthenticatorMakeCredential")
	procWebAuthNAuthenticatorGetAssertion   = webauthnDLL.NewProc("WebAuthNAuthenticatorGetAssertion")
	procWebAuthNFreeCredentialAttestation   = webauthnDLL.NewProc("WebAuthNFreeCredentialAttestation")
	procWebAuthNFreeAssertion               = webauthnDLL.NewProc("WebAuthNFreeAssertion")
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
	cbFirst uint32 // 0
	_       uint32 // 4  padding
	pbFirst *byte  // 8
	//lint:ignore U1000 ABI layout of winwebauthn.h; RestoreSafe sends only the first salt
	cbSecond uint32 // 16
	_        uint32 // 20 padding
	//lint:ignore U1000 ABI layout of winwebauthn.h; RestoreSafe sends only the first salt
	pbSecond *byte // 24
}

// webauthnHmacSecretSaltValues matches WEBAUTHN_HMAC_SECRET_SALT_VALUES. Size: 24 bytes.
type webauthnHmacSecretSaltValues struct {
	pGlobalHmacSalt *webauthnHmacSecretSalt // 0
	//lint:ignore U1000 ABI layout of winwebauthn.h; RestoreSafe sends only the global salt
	cCredWithHmacSecretSaltList uint32 // 8
	_                           uint32 // 12 padding
	//lint:ignore U1000 ABI layout of winwebauthn.h; RestoreSafe sends only the global salt
	pCredWithHmacSecretSaltList unsafe.Pointer // 16
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
