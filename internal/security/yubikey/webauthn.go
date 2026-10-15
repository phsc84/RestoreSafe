//go:build windows

package yubikey

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"runtime"
	"sync/atomic"
	"unsafe"

	"github.com/phsc84/restoresafe/internal/problem"
	"golang.org/x/sys/windows"
)

//lint:file-ignore ST1003 names follow winwebauthn.h, so they match the Windows API documentation

// ── Internal WebAuthn implementation ─────────────────────────────────────────

func validateFIDO2Secret(secret []byte) error {
	if len(secret) != fido2SaltSize {
		return fmt.Errorf("FIDO2 hmac-secret returned %d bytes, want %d", len(secret), fido2SaltSize)
	}
	return nil
}

// webauthnVersionFn returns the API version of webauthn.dll, or the error of
// loading it; replaced in tests.
var webauthnVersionFn = func() (uint32, error) {
	if err := webauthnDLL.Load(); err != nil {
		return 0, err
	}
	ver, _, _ := procWebAuthNGetApiVersionNumber.Call()
	return uint32(ver), nil
}

func checkWebAuthnAPIVersion() error {
	ver, err := webauthnVersionFn()
	if err == nil {
		fido2Log("webauthn.dll API version: %d (minimum required: %d)", ver, webauthnMinAPIVersion)
	}
	return webauthnVersionError(ver, err)
}

// webauthnVersionError returns nil when ver is new enough, and otherwise
// what Windows lacks.
func webauthnVersionError(ver uint32, loadErr error) error {
	if loadErr != nil {
		return problem.Errorf("webauthn.dll not available: %w.", loadErr).WithRemedy("Windows 11 version 22H2 or later is required.")
	}
	if ver < webauthnMinAPIVersion {
		return problem.Errorf("webauthn.dll API version %d is too old (need %d+).", ver, webauthnMinAPIVersion).WithRemedy("Windows 11 version 22H2 or later is required.")
	}
	return nil
}

// parentWindow is the window the Windows Security dialogs (PIN, touch) belong
// to; 0 means no owner window.
var parentWindow atomic.Uintptr

// SetParentWindow sets the window that the Windows Security dialogs of
// YubiKey operations belong to, so they appear in front of it and are modal
// to it. The window passes its main window; 0 means no owner window.
func SetParentWindow(hwnd uintptr) {
	parentWindow.Store(hwnd)
}

// dialogParent returns the parent window for the WebAuthn calls.
func dialogParent() uintptr {
	return parentWindow.Load()
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

// hresultNTEExists is NTE_EXISTS, which Windows returns when the authenticator
// already holds a credential from the exclude list.
const hresultNTEExists = 0x8009000F

// The HRESULTs of a cancel: NTE_USER_CANCELLED, and ERROR_CANCELLED as an
// HRESULT, which WebAuthn is known to return as well.
const (
	hresultNTEUserCancelled = 0x80090036
	hresultCancelled        = 0x800704C7
)

// HRESULTError is a WebAuthn call that Windows ended with an error.
type HRESULTError struct {
	// Op is the WebAuthn function that failed.
	Op   string
	Code uint32
}

func (e *HRESULTError) Error() string {
	return fmt.Sprintf("%s: %s", e.Op, webauthnErrorString(uintptr(e.Code)))
}

// Cancelled tells whether the user cancelled the Windows Security dialog.
func (e *HRESULTError) Cancelled() bool {
	return e.Code == hresultNTEUserCancelled || e.Code == hresultCancelled
}

// credentialList builds a WEBAUTHN_CREDENTIALS list. The returned slices must
// stay alive until the WebAuthn call returns.
func credentialList(credIDs [][]byte) (webauthnCredentials, []webauthnCredential) {
	if len(credIDs) == 0 {
		return webauthnCredentials{}, nil
	}
	credTypeW := windows.StringToUTF16Ptr(webAuthnCredTypePublicKey)
	creds := make([]webauthnCredential, len(credIDs))
	for i, id := range credIDs {
		creds[i] = webauthnCredential{
			dwVersion:          webauthnCredentialVersion,
			cbId:               uint32(len(id)),
			pbId:               &id[0],
			pwszCredentialType: credTypeW,
		}
	}
	return webauthnCredentials{cCredentials: uint32(len(creds)), pCredentials: &creds[0]}, creds
}

// realMakeCredential calls WebAuthNAuthenticatorMakeCredential to create a new
// non-resident FIDO2 credential with the hmac-secret extension enabled.
// The returned credential ID is an opaque blob that the authenticator uses to
// derive the hmac-secret on subsequent GetAssertion calls. An authenticator
// holding one of the exclude credentials refuses the registration.
func realMakeCredential(exclude [][]byte) (credID []byte, err error) {
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

	excludeList, excludeCreds := credentialList(exclude)
	defer runtimeKeepAlive(excludeCreds, exclude)

	opts := webauthnMakeCredentialOptions{
		dwVersion:                         webauthnMakeCredOptVersion,
		dwTimeoutMilliseconds:             fido2TimeoutMs,
		CredentialList:                    excludeList,
		Extensions:                        exts,
		dwAuthenticatorAttachment:         webauthnAttachmentCrossPlatform,
		dwUserVerificationRequirement:     webauthnUVRequired,
		dwAttestationConveyancePreference: webauthnAttestationNone,
	}

	var attestation *webauthnCredAttestation
	hr, _, _ := procWebAuthNAuthenticatorMakeCredential.Call(
		dialogParent(),
		uintptr(unsafe.Pointer(&rp)),
		uintptr(unsafe.Pointer(&user)),
		uintptr(unsafe.Pointer(&credParams)),
		uintptr(unsafe.Pointer(&cd)),
		uintptr(unsafe.Pointer(&opts)),
		uintptr(unsafe.Pointer(&attestation)),
	)
	if hr == hresultNTEExists && len(exclude) > 0 {
		return nil, ErrAlreadyRegistered
	}
	if hr != 0 {
		return nil, problem.Errorf("%w.", &HRESULTError{Op: "WebAuthNAuthenticatorMakeCredential", Code: uint32(hr)}).WithRemedy("Ensure the YubiKey is connected, supports FIDO2 hmac-secret, and has a FIDO2 PIN configured.")
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
// extension to derive a 32-byte secret for salt. credIDs is the allow list:
// the connected authenticator answers with whichever credential it holds, and
// the index of that credential is returned.
func realGetHmacSecret(credIDs [][]byte, salt []byte) (int, []byte, error) {
	if len(salt) != fido2SaltSize {
		return 0, nil, fmt.Errorf("hmac-secret salt must be %d bytes, got %d", fido2SaltSize, len(salt))
	}
	if len(credIDs) == 0 {
		return 0, nil, fmt.Errorf("no credential to authenticate with")
	}

	clientDataBytes, err := buildClientData("webauthn.get")
	if err != nil {
		return 0, nil, fmt.Errorf("failed to build client data: %w", err)
	}

	rpIdW := windows.StringToUTF16Ptr(fido2RPID)

	hashAlgW := windows.StringToUTF16Ptr(webAuthnHashAlgSHA256)
	cd := webauthnClientData{
		dwVersion:        webauthnClientDataVersion,
		cbClientDataJSON: uint32(len(clientDataBytes)),
		pbClientDataJSON: &clientDataBytes[0],
		pwszHashAlgId:    hashAlgW,
	}

	allowedList, allowedCreds := credentialList(credIDs)
	defer runtimeKeepAlive(allowedCreds, credIDs)

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
		dialogParent(),
		uintptr(unsafe.Pointer(rpIdW)),
		uintptr(unsafe.Pointer(&cd)),
		uintptr(unsafe.Pointer(&opts)),
		uintptr(unsafe.Pointer(&assertion)),
	)
	if hr != 0 {
		return 0, nil, problem.Errorf("%w.", &HRESULTError{Op: "WebAuthNAuthenticatorGetAssertion", Code: uint32(hr)}).WithRemedy("Ensure a YubiKey registered for this backup is connected.")
	}
	if assertion == nil || assertion.pHmacSecret == nil {
		return 0, nil, fmt.Errorf("WebAuthNAuthenticatorGetAssertion returned no hmac-secret output")
	}
	defer procWebAuthNFreeAssertion.Call(uintptr(unsafe.Pointer(assertion)))
	hmacSecret := assertion.pHmacSecret
	if hmacSecret.cbFirst == 0 {
		return 0, nil, fmt.Errorf("WebAuthNAuthenticatorGetAssertion returned empty hmac-secret first output")
	}

	index, err := answeringCredential(credIDs, assertion.Credential)
	if err != nil {
		return 0, nil, err
	}
	out := make([]byte, hmacSecret.cbFirst)
	copy(out, unsafe.Slice(hmacSecret.pbFirst, hmacSecret.cbFirst))
	fido2Log("hmac-secret output length: %d bytes, credential index %d", len(out), index)
	return index, out, nil
}

// answeringCredential returns the index in credIDs of the credential the
// authenticator used. With a single allowed credential an empty response
// credential is accepted.
func answeringCredential(credIDs [][]byte, used webauthnCredential) (int, error) {
	if used.cbId == 0 || used.pbId == nil {
		if len(credIDs) == 1 {
			return 0, nil
		}
		return 0, fmt.Errorf("WebAuthNAuthenticatorGetAssertion did not report which YubiKey answered")
	}
	usedID := unsafe.Slice(used.pbId, used.cbId)
	for i, id := range credIDs {
		if bytes.Equal(id, usedID) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("YubiKey answered with an unknown credential")
}

// runtimeKeepAlive keeps Go memory referenced by raw pointers passed to the
// WebAuthn API alive until the call has returned.
func runtimeKeepAlive(values ...any) {
	for _, v := range values {
		runtime.KeepAlive(v)
	}
}
