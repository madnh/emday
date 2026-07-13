// Package buildinfo carries version metadata stamped at build time via -ldflags -X.
package buildinfo

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func String() string {
	return Version + " (" + Commit + ", " + Date + ")"
}
