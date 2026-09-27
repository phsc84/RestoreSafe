package cryptox

// ZeroBytes overwrites b with zeros to reduce the window in which a password
// is present in process memory. Call it as soon as the password is no longer
// needed; use defer for correctness on all return paths.
func ZeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
