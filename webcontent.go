package webcontent

import "embed"

// WebContent holds the built frontend assets, embedded into the
// binary at compile time. The server uses this to serve the SPA
// without needing the dist/ directory on disk in production.
//
// Run 'bun build --outdir=dist --production ./client/index.html'
// before building the Go binary so the embed pattern matches.
//
// Matched files:
//   dist/index-*.js   (the hashed JS bundle)
//   dist/index-*.css  (the hashed CSS bundle)
//   dist/index.html   (the HTML shell)
//
// The pattern is at the project root so the embed can find the
// files without '..' (which //go:embed doesn't allow).
//
//go:embed dist/index-*.js dist/index-*.css dist/index.html
var WebContent embed.FS
