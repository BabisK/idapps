// Package catalogue provides the schema embedded in the launcher.
package catalogue

import _ "embed"

// Schema is the contributor-facing application descriptor schema.
//
//go:embed catalogue.schema.json
var Schema []byte
