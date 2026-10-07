package archive

import (
	"strings"
	"testing"
)

func TestExtendedPathPrefixesLongAbsolutePaths(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 250)
	for _, c := range []struct{ in, want string }{
		{`C:\short\file.txt`, `C:\short\file.txt`},
		{`relative\` + long, `relative\` + long},
		{`C:\dir\` + long, `\\?\C:\dir\` + long},
		{`C:\dir\.\sub\..\` + long, `\\?\C:\dir\` + long},
		{`\\server\share\` + long, `\\?\UNC\server\share\` + long},
		{`\\?\C:\dir\` + long, `\\?\C:\dir\` + long},
	} {
		if got := extendedPath(c.in); got != c.want {
			t.Errorf("extendedPath(%.40q...) = %.50q..., want %.50q...", c.in, got, c.want)
		}
	}
}
