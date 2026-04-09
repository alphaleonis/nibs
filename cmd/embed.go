package cmd

import "io/fs"

// WebDistFS holds the embedded frontend filesystem. When non-nil, the serve
// command will serve the SPA frontend for non-API routes.
// Must be set before Execute() is called. Not safe for concurrent use.
var WebDistFS fs.FS
