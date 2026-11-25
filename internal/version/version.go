// Package version holds the SemVer baked into the binaries.
// Override at link time with -X mcon/internal/version.Version=<VERSION>.
package version

// Version is the semantic version of this build.
// The default matches the repository VERSION file for development builds.
var Version = "0.1.3"
