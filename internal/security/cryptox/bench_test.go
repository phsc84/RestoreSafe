package cryptox

import (
	"bytes"
	"io"
	"testing"
)

const benchStreamSize = 64 << 20

func BenchmarkEncryptStream(b *testing.B) {
	key := bytes.Repeat([]byte{7}, KeyLen)
	b.SetBytes(benchStreamSize)
	b.ReportAllocs()
	for b.Loop() {
		if err := EncryptStream(io.Discard, io.LimitReader(zeroReader{}, benchStreamSize), key, []byte("aad")); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecryptStream(b *testing.B) {
	key := bytes.Repeat([]byte{7}, KeyLen)
	var encrypted bytes.Buffer
	if err := EncryptStream(&encrypted, io.LimitReader(zeroReader{}, benchStreamSize), key, []byte("aad")); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(benchStreamSize)
	b.ReportAllocs()
	for b.Loop() {
		if err := DecryptStream(io.Discard, bytes.NewReader(encrypted.Bytes()), key, []byte("aad")); err != nil {
			b.Fatal(err)
		}
	}
}
