// Package restoresafe holds the files of the repository root that the
// program embeds.
package restoresafe

import _ "embed"

// ConfigSample is config-SAMPLE.yaml. config.AddMissing takes the
// explanation of each setting it adds from it.
//
//go:embed config-SAMPLE.yaml
var ConfigSample []byte
