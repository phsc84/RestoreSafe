//go:build windows

package yubikey

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// webauthnStructs lists every struct that is passed to webauthn.dll.
var webauthnStructs = map[string]reflect.Type{
	"webauthnRPEntity":              reflect.TypeFor[webauthnRPEntity](),
	"webauthnUserEntity":            reflect.TypeFor[webauthnUserEntity](),
	"webauthnCoseCredParam":         reflect.TypeFor[webauthnCoseCredParam](),
	"webauthnCoseCredParams":        reflect.TypeFor[webauthnCoseCredParams](),
	"webauthnClientData":            reflect.TypeFor[webauthnClientData](),
	"webauthnCredential":            reflect.TypeFor[webauthnCredential](),
	"webauthnCredentials":           reflect.TypeFor[webauthnCredentials](),
	"webauthnExtension":             reflect.TypeFor[webauthnExtension](),
	"webauthnExtensions":            reflect.TypeFor[webauthnExtensions](),
	"webauthnMakeCredentialOptions": reflect.TypeFor[webauthnMakeCredentialOptions](),
	"webauthnCredAttestation":       reflect.TypeFor[webauthnCredAttestation](),
	"webauthnHmacSecretSalt":        reflect.TypeFor[webauthnHmacSecretSalt](),
	"webauthnHmacSecretSaltValues":  reflect.TypeFor[webauthnHmacSecretSaltValues](),
	"webauthnGetAssertionOptions":   reflect.TypeFor[webauthnGetAssertionOptions](),
	"webauthnAssertion":             reflect.TypeFor[webauthnAssertion](),
}

var (
	sizeComment   = regexp.MustCompile(`Size: (\d+) bytes`)
	offsetComment = regexp.MustCompile(`^//\s*(\d+)\b`)
)

// TestWebAuthnStructLayout checks every webauthn* struct in fido2.go against
// the layout of winwebauthn.h written in its comments: the size in the doc
// comment and the offset at the start of each field's comment. A wrong
// offset makes the YubiKey fail only on real hardware, which CI lacks.
func TestWebAuthnStructLayout(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fido2.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts := spec.(*ast.TypeSpec)
			st, ok := ts.Type.(*ast.StructType)
			if !ok || !strings.HasPrefix(ts.Name.Name, "webauthn") {
				continue
			}
			name := ts.Name.Name
			seen[name] = true
			typ, ok := webauthnStructs[name]
			if !ok {
				t.Errorf("%s is missing from webauthnStructs", name)
				continue
			}
			if m := sizeComment.FindStringSubmatch(gen.Doc.Text()); m != nil {
				if want, _ := strconv.Atoi(m[1]); typ.Size() != uintptr(want) {
					t.Errorf("%s: size %d, comment says %d", name, typ.Size(), want)
				}
			}
			i := 0
			for _, field := range st.Fields.List {
				for range max(len(field.Names), 1) {
					f := typ.Field(i)
					i++
					if field.Comment == nil {
						t.Errorf("%s.%s: no offset comment", name, f.Name)
						continue
					}
					m := offsetComment.FindStringSubmatch(field.Comment.List[0].Text)
					if m == nil {
						t.Errorf("%s.%s: comment %q does not start with the offset", name, f.Name, field.Comment.List[0].Text)
						continue
					}
					if want, _ := strconv.Atoi(m[1]); f.Offset != uintptr(want) {
						t.Errorf("%s.%s: offset %d, comment says %d", name, f.Name, f.Offset, want)
					}
				}
			}
		}
	}
	for name := range webauthnStructs {
		if !seen[name] {
			t.Errorf("%s is not declared in fido2.go", name)
		}
	}
}
