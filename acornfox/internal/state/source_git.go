package state

// SourceGit is the deployment source kind for a public Git repository the
// server clones itself (see GitSource in internal/client). SourceRef holds
// "<url>#<ref>" where <ref> may be empty; SourceDigest is sha256(url+"#"+ref).
const SourceGit = "git"
