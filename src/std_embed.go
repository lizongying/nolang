package nolang

import "embed"

//go:embed std
var StdFS embed.FS

//go:embed js
var JsFS embed.FS
