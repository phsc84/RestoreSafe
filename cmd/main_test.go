package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigPathFromArgs(t *testing.T) {
	t.Parallel()
	exeDir := `C:\Tools\RestoreSafe`
	custom := `D:\Configs\home.yaml`
	cases := []struct {
		args    []string
		want    string
		wantErr string
	}{
		{nil, filepath.Join(exeDir, "config.yaml"), ""},
		{[]string{"-config=" + custom}, custom, ""},
		{[]string{"--config=" + custom}, custom, ""},
		{[]string{"-config=" + `D:\Configs\..\Configs\home.yaml`}, custom, ""},
		{[]string{"-config", custom}, "", "equals form only"},
		{[]string{"-config="}, "", "requires an absolute path"},
		{[]string{"-config=home.yaml"}, "", "requires an absolute path"},
	}
	for _, c := range cases {
		got, err := configPathFromArgs(c.args, exeDir)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%v: error %v, want %q", c.args, err, c.wantErr)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%v: got %q, %v; want %q", c.args, got, err, c.want)
		}
	}
}
