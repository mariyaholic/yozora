// Package version holds the release version stamped in at build time.
package version

// Version is overwritten by the release build with
// -ldflags "-X uika-resonance/internal/version.Version=0.1.N".
// Local and pull-request builds keep "dev".
var Version = "dev"
