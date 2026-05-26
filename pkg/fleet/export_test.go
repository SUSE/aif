package fleet

// BuildBundleCRForTest re-exports buildBundleCR for black-box example tests.
var BuildBundleCRForTest = buildBundleCR

// GitRepoNameForTest re-exports gitRepoName for black-box tests.
var GitRepoNameForTest = gitRepoName

// GitRepoPathForTest re-exports gitRepoPath for black-box tests.
var GitRepoPathForTest = gitRepoPath

// BuildGitRepoCRForTest re-exports buildGitRepoCR for tests that need to
// inspect the full CR shape (e.g., the engine integration test).
var BuildGitRepoCRForTest = buildGitRepoCR

// MirrorGitRepoStatusForTest re-exports mirrorGitRepoStatus for black-box tests.
var MirrorGitRepoStatusForTest = mirrorGitRepoStatus
