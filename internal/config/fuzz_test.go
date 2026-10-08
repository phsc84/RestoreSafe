package config

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParse feeds arbitrary bytes as config.yaml. An accepted configuration
// keeps Argon2 within its bounds, so that no file can make RestoreSafe
// derive keys with more than 4 GiB of memory.
func FuzzParse(f *testing.F) {
	sample, err := os.ReadFile(filepath.Join("..", "..", "config-SAMPLE.yaml"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(sample)
	f.Add([]byte("argon2:\n  time: 99\n  memory_mb: 999999\n  threads: 9999\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := parse(data)
		if err != nil {
			return
		}
		a := cfg.Argon2
		if a.Time < Argon2MinTime || a.Time > Argon2MaxTime || a.MemoryMB < Argon2MinMemoryMB || a.MemoryMB > Argon2MaxMemoryMB || a.Threads < Argon2MinThreads || a.Threads > Argon2MaxThreads {
			t.Fatalf("accepted Argon2 parameters out of bounds: %+v", a)
		}
	})
}
