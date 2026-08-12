// Package version carries Deployer's version number.
//
// The scheme is vMAJOR.MINOR.PATCH where the patch number is the repository's
// commit count — every commit is a patch release, so `v1.0.42` is the 42nd
// commit on the 1.0 line. Major and minor are declared here in source and
// bumped by hand; the patch number can only come from git, which a compiled
// binary has no access to, so it is stamped at link time instead:
//
//	go build -ldflags "-X github.com/chinmay28/deployer/server/internal/version.Patch=$(git rev-list --count HEAD)"
//
// `make build` does this for you via scripts/version.mjs, which is also where
// the PWA's build reads Major/Minor from — keep the two constants below in a
// form that file's regex can still find.
package version

import "strconv"

// Major and minor version. Bump these by hand.
const (
	Major = 1
	Minor = 0
)

// Patch is the repository's commit count, stamped at link time (see the
// package comment). A bare `go build` leaves it at "0": patch 0 means an
// unstamped development build, never a release.
var Patch = "0"

// Stamped reports whether the patch number came from git. It is false for a
// bare `go build`, and for a build made where git could not be asked — from a
// tarball, or from a shallow clone (see scripts/version.mjs).
func Stamped() bool { return Patch != "0" }

// String renders the full version, `v`-prefixed to match how the project tags
// releases. This is the one rendering: it's what /api/health reports, what
// /api/self hands Settings, and what the PWA shows under its name.
func String() string {
	return "v" + strconv.Itoa(Major) + "." + strconv.Itoa(Minor) + "." + Patch
}
