package catalog

// What the format accepts (since BasePlatform) but this platform does not
// carry out yet. Nothing is in it now: every writing of the format is
// carried out. The last ones went with the B section's runtime: a limit on
// how many consumers a service takes (max_consumers), deleting a consumer's
// data (purge), stopping when an old credential fails its check
// (on_check_fail: stop), a readiness check (ready) and a need that takes a
// service no one else uses (dedicated) are all done by appinstall now; so
// are command steps, issue_vars, issued.* in a value template, tcp
// endpoints, exec and tcp health checks and the internal exposure.
//
// The mechanism stays for what comes next. This list is the one place that
// says what is pending. Lint warns about each use (CAT-LINT-26) and the
// install engine refuses a manifest that has one before anything is created
// (CAT-INST-23), so such a manifest is never half installed, nor run as if
// the field were not there. A writing added to the format before the
// platform carries it out goes here (each its own line, with the tests that
// list it); when the platform carries it out, it is dropped: lint stops
// warning and the engine stops refusing at the same time.

// Pending is one use of such a writing: Where points at it inside the
// manifest (as lint paths do), What says what it is, for people.
type Pending struct {
	Where string `json:"where"`
	What  string `json:"what"`
}

// PendingFeatures lists, in manifest order, what m writes that this
// platform does not carry out yet (see above). It is empty for a manifest
// this platform can install as written, which is every manifest now.
func PendingFeatures(m *Manifest) []Pending {
	return nil
}
