package blueprint

import "errors"

var (
	ErrInvalidVersion   = errors.New("invalid semver version")
	ErrSkippedNonSemVer = errors.New("non-semver version skipped")
)
