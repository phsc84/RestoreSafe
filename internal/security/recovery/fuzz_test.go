package recovery

import "testing"

// FuzzParse checks that a code Parse accepts is parsed the same from its
// display form.
func FuzzParse(f *testing.F) {
	code, err := Generate()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(code.Display())
	f.Add([]byte("abcde fghjk"))
	f.Fuzz(func(t *testing.T, input []byte) {
		c, err := Parse(input)
		if err != nil {
			return
		}
		again, err := Parse(c.Display())
		if err != nil || !sameCode(again, c) {
			t.Fatalf("%q parsed, its display form %q does not (%v)", input, c.Display(), err)
		}
	})
}
