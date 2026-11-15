package apiserver

import (
	"errors"
	"net/http"

	"github.com/acornfox/acornfox/internal/appinstall"
)

// Services: what this machine can offer its apps (internal/appinstall,
// services.go and registry.go). The rules are the engine's; these handlers
// only decode and encode.
//
//	GET    /v1/services/kinds         service types of the catalogs in use (items, has_test)
//	GET    /v1/services               every service: values (secrets masked), status, used_by
//	GET    /v1/services/{name}        one service (the same object, not wrapped)
//	POST   /v1/services               register one {"name","kind","values":{item: "text"}} → 201
//	PATCH  /v1/services/{name}        change items {"values":{item: "text", item: ""}} ("" clears);
//	                                  the answer carries "notes" (who has to apply the settings again)
//	POST   /v1/services/{name}/test   try it the way its kind says → {"ok","detail","code","target","skipped"}
//	DELETE /v1/services/{name}        204; refused with CAT-SVC-04 and "used_by" while apps use it
//
// Errors: {"error":{"code","message","hint"}} with a CAT-SVC-* code (or
// CAT-INST-20); CAT-SVC-04 adds "used_by":[{"app","use"}]. A name that is
// not registered is 404 "not_found". Secrets never leave masked: there is
// no parameter that shows them. Routes sit next to the app center's, on the
// trusted socket and the logged-in console only.

func (s *server) registerServices(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/services/kinds", s.serviceKinds)
	mux.HandleFunc("GET /v1/services", s.listServices)
	mux.HandleFunc("GET /v1/services/{name}", s.getService)
	mux.HandleFunc("POST /v1/services", s.addService)
	mux.HandleFunc("PATCH /v1/services/{name}", s.patchService)
	mux.HandleFunc("POST /v1/services/{name}/test", s.testService)
	mux.HandleFunc("DELETE /v1/services/{name}", s.removeService)
}

type serviceErrorBody struct {
	Error struct {
		Code    string                  `json:"code"`
		Message string                  `json:"message"`
		Hint    string                  `json:"hint,omitempty"`
		UsedBy  []appinstall.ServiceUse `json:"used_by,omitempty"`
	} `json:"error"`
}

func writeServiceFault(w http.ResponseWriter, err error) {
	var none *appinstall.NoServiceError
	if errors.As(err, &none) {
		writeError(w, http.StatusNotFound, "not_found", none.Error())
		return
	}
	var b serviceErrorBody
	var inUse *appinstall.ServiceInUseFault
	if errors.As(err, &inUse) {
		b.Error.UsedBy = inUse.UsedBy
	}
	f := appinstall.AsFault(err)
	b.Error.Code, b.Error.Message, b.Error.Hint = f.Code, f.Message, f.Hint
	writeJSON(w, f.HTTPStatus(), b)
}

func (s *server) serviceKinds(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	v, err := s.appCenter.ServiceKinds(r.Context())
	if err != nil {
		writeServiceFault(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kinds": v})
}

func (s *server) listServices(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	v, err := s.appCenter.Services(r.Context())
	if err != nil {
		writeServiceFault(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, v)
}

func (s *server) getService(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	v, err := s.appCenter.Service(r.Context(), r.PathValue("name"))
	if err != nil {
		writeServiceFault(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, v)
}

func (s *server) addService(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	var req appinstall.AddServiceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeServiceFault(w, appinstall.BadServiceBody(err))
		return
	}
	v, err := s.appCenter.AddService(r.Context(), req)
	if err != nil {
		writeServiceFault(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (s *server) patchService(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	var req appinstall.PatchServiceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeServiceFault(w, appinstall.BadServiceBody(err))
		return
	}
	v, err := s.appCenter.PatchService(r.Context(), r.PathValue("name"), req)
	if err != nil {
		writeServiceFault(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *server) testService(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	v, err := s.appCenter.TestService(r.Context(), r.PathValue("name"))
	if err != nil {
		writeServiceFault(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *server) removeService(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	res, err := s.appCenter.RemoveService(r.Context(), r.PathValue("name"))
	if err != nil {
		writeServiceFault(w, err)
		return
	}
	// Notes (apps deleted earlier whose bindings went with it) need a body;
	// nothing to say is a plain 204, as before.
	if len(res.Notes) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
