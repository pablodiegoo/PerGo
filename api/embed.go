// Package api embeds the OpenAPI 3.1 specification and standalone documentation assets.
package api

import (
	_ "embed"
)

// OpenAPIYAML contains the raw OpenAPI 3.1 YAML specification.
//
//go:embed openapi.yaml
var OpenAPIYAML []byte

// ScalarJS contains the offline standalone Scalar API reference bundle.
//
//go:embed scalar.standalone.js
var ScalarJS []byte
