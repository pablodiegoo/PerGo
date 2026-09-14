// Package api embeds the OpenAPI 3.1 specification and standalone documentation assets.
package api

import (
	_ "embed"
)

// OpenAPIYAML contains the raw OpenAPI 3.1 YAML specification.
//
//go:embed openapi.yaml
var OpenAPIYAML []byte

// OpenAPIJSON contains the raw OpenAPI 3.1 JSON specification.
//
//go:embed openapi.json
var OpenAPIJSON []byte

// ScalarJS contains the offline standalone Scalar API reference bundle.
//
//go:embed scalar.standalone.js
var ScalarJS []byte

// LLMsTxt contains the curated agent index conforming to llmstxt.org v2 format.
//
//go:embed llms.txt
var LLMsTxt []byte

// LLMsFullTxt contains the complete developer documentation in a single markdown payload.
//
//go:embed llms-full.txt
var LLMsFullTxt []byte

