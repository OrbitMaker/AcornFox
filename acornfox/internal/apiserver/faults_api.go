package apiserver

// The fault catalog (internal/faults), read-only:
//
//	GET /v1/faults          every entry, with the groups, tiers and levels
//	GET /v1/faults/{code}   one entry; an unknown code is 404 fault_unknown
//	                        with the fallback entry ANY-00 in "fallback"
//
// The same text as `acornfox faults [code] --json` and docs/faults.md. Codes
// match like the CLI: upper-case ones in any case, lower-case ones exactly.

import (
	"net/http"

	"github.com/acornfox/acornfox/internal/faults"
)

func (s *server) getFaults(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"faults": faults.Catalog, "groups": faults.Groups, "tiers": faults.TierName, "levels": faults.LevelName, "evidence": faults.Evidence,
	})
}

func (s *server) getFault(w http.ResponseWriter, r *http.Request) {
	f, ok := faults.Find(r.PathValue("code"))
	if !ok {
		var body errorBody
		body.Error.Code = "fault_unknown"
		body.Error.Message = "故障目录里没有 " + r.PathValue("code") + "：按兜底条目 " + faults.FallbackCode + " 处理"
		writeJSON(w, http.StatusNotFound, map[string]any{"error": body.Error, "fallback": f})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fault": f})
}
