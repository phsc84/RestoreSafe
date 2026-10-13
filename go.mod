module github.com/phsc84/restoresafe

go 1.27.2

require (
	go.yaml.in/yaml/v3 v3.0.5
	golang.org/x/crypto v0.58.0
	golang.org/x/sys v0.49.0
)

require (
	github.com/BurntSushi/toml v1.6.0 // indirect
	github.com/akavel/rsrc v0.10.2 // indirect
	github.com/josephspurrier/goversioninfo v1.7.0 // indirect
	golang.org/x/exp/typeparams v0.0.0-20261011100332-faecfb24eb49 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/telemetry v0.0.0-20261013142427-f87725b73f11 // indirect
	golang.org/x/tools v0.52.0 // indirect
	golang.org/x/vuln v1.9.0 // indirect
	honnef.co/go/tools v0.8.2 // indirect
)

tool (
	github.com/josephspurrier/goversioninfo/cmd/goversioninfo
	golang.org/x/tools/cmd/deadcode
	golang.org/x/vuln/cmd/govulncheck
	honnef.co/go/tools/cmd/staticcheck
)
