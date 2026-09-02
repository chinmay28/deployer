# Working in this repository

## The version line follows the branch

The version is `vYEAR.MONTH.PATCH`: `Year` and `Month` are constants in
`server/internal/version/version.go`, and the patch number is the commit count.
The line is the month a branch opened in, so a release says when its work began.

**Before the first commit on any branch, run `make bump-version`** (or
`node scripts/version.mjs --bump`). It sets `Year` and `Month` to the current
month in UTC and prints whether anything moved; commit the change with the rest
of the work. Do this without being asked, and do not bump again later on the
same branch — the line is when the work started, not when it finished. Never set
the constants from the build clock at build time: a rebuild of an old tree must
still report what it originally shipped.

`make version` prints the version this tree would build as.
