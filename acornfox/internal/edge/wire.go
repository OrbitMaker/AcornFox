package edge

// The management protocol between the acornfox program (package edgeclient)
// and the adapter inside the entry proxy container (cmd/afx-edge, package
// edgeadapter). Plain HTTP with JSON bodies on the adapter's own management
// endpoint, which listens on this computer only. Every request must carry
// TokenHeader; a request without it, or with a wrong one, gets 401 and
// nothing else (contract 7).
//
//	PUT    PathConfig     body Config            -> 200 VersionReply   (Apply)
//	GET    PathVersion                            -> 200 VersionReply   (Version; 503 while the adapter is starting)
//	PUT    PathChallenge  body ChallengeRequest   -> 204                (SetChallenge)
//	DELETE PathChallenge                          -> 204                (ClearChallenge)
//	GET    PathHealth                             -> 200 Health         (Health)
//
// Errors are a non-2xx status with ErrorReply. 400 means the configuration
// was refused as written (it is not retried as is); 5xx means the proxy could
// not apply it right now.
const (
	TokenHeader   = "X-AcornFox-Token"
	PathConfig    = "/v1/config"
	PathVersion   = "/v1/version"
	PathChallenge = "/v1/challenge"
	PathHealth    = "/v1/health"
)

// VersionReply answers Apply and Version.
type VersionReply struct {
	Version string `json:"version"`
}

// ChallengeRequest is the body of SetChallenge.
type ChallengeRequest struct {
	Host string `json:"host"`
	Path string `json:"path"`
	Body string `json:"body"`
}

// ErrorReply is the body of every error answer.
type ErrorReply struct {
	Error string `json:"error"`
}
