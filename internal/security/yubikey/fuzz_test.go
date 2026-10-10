package yubikey

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// FuzzParseChallengeJSON checks that accepted challenge data survives a
// round trip through JSON.
func FuzzParseChallengeJSON(f *testing.F) {
	seed, _ := json.Marshal(ChallengeData{
		Version: 1,
		CredID:  base64.StdEncoding.EncodeToString(make([]byte, 64)),
		Salt:    base64.StdEncoding.EncodeToString(make([]byte, fido2SaltSize)),
	})
	f.Add(string(seed))
	f.Add(`{"version":1}`)
	f.Fuzz(func(t *testing.T, s string) {
		cd, err := ParseChallengeJSON(s)
		if err != nil {
			return
		}
		again, err := json.Marshal(cd)
		if err != nil {
			t.Fatal(err)
		}
		if cd2, err := ParseChallengeJSON(string(again)); err != nil || cd2 != cd {
			t.Fatalf("%q parsed as %+v, which does not parse again (%v)", s, cd, err)
		}
	})
}
