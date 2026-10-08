package cryptox

import (
	"bytes"
	"testing"
)

// FuzzDecryptStream feeds arbitrary bytes as a chunk stream. Whatever
// DecryptStream accepts must be exactly what EncryptStream writes for the
// plaintext it returned: the framing admits no second encoding.
func FuzzDecryptStream(f *testing.F) {
	key := bytes.Repeat([]byte{7}, KeyLen)
	aad := []byte("aad")
	for _, plain := range [][]byte{nil, []byte("restoresafe")} {
		var seed bytes.Buffer
		if err := EncryptStream(&seed, bytes.NewReader(plain), key, aad); err != nil {
			f.Fatal(err)
		}
		f.Add(seed.Bytes())
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var plain bytes.Buffer
		if err := DecryptStream(&plain, bytes.NewReader(data), key, aad); err != nil {
			return
		}
		var again bytes.Buffer
		if err := EncryptStream(&again, bytes.NewReader(plain.Bytes()), key, aad); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(again.Bytes(), data) {
			t.Fatalf("DecryptStream accepted %x, which EncryptStream does not write", data)
		}
	})
}
