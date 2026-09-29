package webcontent

import "embed"

// WebContent holds the built frontend assets, embedded into the
// binary at compile time. The server uses this to serve the SPA
// without needing the dist/ directory on disk in production.
//
// Run 'mise run build_client_prod' (Vite) before building the Go
// binary so the embed pattern matches.
//
// Matched files:
//   dist/index.html   (the HTML shell)
//   dist/assets/*     (hashed JS/CSS bundles; Vite's assetsDir)
//
// The pattern is at the project root so the embed can find the
// files without '..' (which //go:embed doesn't allow).
//
//go:embed dist/index.html dist/assets
var WebContent embed.FS
