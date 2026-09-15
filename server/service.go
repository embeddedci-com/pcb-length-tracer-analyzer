package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Service is the mounted control plane.
type Service struct {
	deps Deps
}

// New builds a service. A nil Store is an error the host should not be able to
// make quietly, so it panics rather than failing on the first request.
func New(d Deps) *Service {
	if d.Store == nil {
		panic("pcb-trace-length-analyzer: Deps.Store is required")
	}
	return &Service{deps: d}
}

// Mount registers the routes under a prefix.
//
// userAuth is the host's own authentication middleware. It has to have run and
// called WithUser before any handler here sees a request: this package does not
// authenticate anybody, it only refuses to act for an identity the host did not
// vouch for. Passing nil leaves the routes unauthenticated, which is only
// appropriate for the standalone harness.
func (s *Service) Mount(mux *http.ServeMux, prefix string, userAuth func(http.HandlerFunc) http.HandlerFunc) {
	if userAuth == nil {
		userAuth = func(h http.HandlerFunc) http.HandlerFunc { return h }
	}
	p := prefix

	// Every response here is per-user, and one of them is a board file. No
	// cache in front of the server may keep any of it. That is not
	// hypothetical: with no Cache-Control the Cloudflare edge decides by file
	// extension, and it has cached EMI's ".bin" artifact responses before.
	handle := func(pattern string, h http.HandlerFunc) { mux.HandleFunc(pattern, noStore(h)) }

	handle("POST "+p+"/pcb-trace-length-analyzer/sessions", userAuth(s.handleUpload))
	handle("GET "+p+"/pcb-trace-length-analyzer/sessions", userAuth(s.handleList))
	handle("GET "+p+"/pcb-trace-length-analyzer/sessions/{id}", userAuth(s.handleGet))
	handle("DELETE "+p+"/pcb-trace-length-analyzer/sessions/{id}", userAuth(s.handleDelete))
	handle("POST "+p+"/pcb-trace-length-analyzer/sessions/{id}/plan", userAuth(s.handlePlan))
	handle("GET "+p+"/pcb-trace-length-analyzer/sessions/{id}/headroom", userAuth(s.handleHeadroom))
	handle("GET "+p+"/pcb-trace-length-analyzer/sessions/{id}/suggested-areas", userAuth(s.handleSuggestAreas))
	handle("POST "+p+"/pcb-trace-length-analyzer/sessions/{id}/route", userAuth(s.handleRoute))
	handle("POST "+p+"/pcb-trace-length-analyzer/sessions/{id}/apply", userAuth(s.handleApply))
	handle("GET "+p+"/pcb-trace-length-analyzer/sessions/{id}/download", userAuth(s.handleDownload))
	handle("GET "+p+"/pcb-trace-length-analyzer/sessions/{id}/preview/board.json", userAuth(s.handlePreviewBoard))
	handle("GET "+p+"/pcb-trace-length-analyzer/sessions/{id}/preview/geometry.bin", userAuth(s.handlePreviewGeometry))
	handle("GET "+p+"/pcb-trace-length-analyzer/sessions/{id}/nets", userAuth(s.handleNets))
	handle("GET "+p+"/pcb-trace-length-analyzer/sessions/{id}/changes", userAuth(s.handleChanges))
	handle("GET "+p+"/pcb-trace-length-analyzer/defaults", userAuth(s.handleDefaults))
}

func noStore(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		h(w, r)
	}
}

// ---- helpers ----

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The header is already out; nothing useful is left to do but note it.
		return
	}
}

func (s *Service) fail(w http.ResponseWriter, r *http.Request, code int, err error) {
	if code >= 500 {
		s.deps.log().Error("autoroute request failed",
			"method", r.Method, "path", r.URL.Path, "err", err)
	}
	writeJSON(w, code, errorBody{Error: err.Error()})
}

// user resolves the caller, refusing anything the host did not vouch for.
//
// What matters is that there is an identity to file a board under and to check
// on the way back out, not that somebody has an account. A host that lets
// people try the tool signed out -- embeddedci-server does, with a signed cookie
// per browser -- vouches for an anonymous identity with an id of its own, and
// that is served. A request with no id at all is refused: every such request
// would be the same "nobody", and one visitor would see another's boards.
func (s *Service) user(w http.ResponseWriter, r *http.Request) (UserIdentity, bool) {
	u, ok := UserFrom(r.Context())
	if !ok || u.UserID == "" {
		s.fail(w, r, http.StatusUnauthorized, errors.New("sign in to analyse a board"))
		return UserIdentity{}, false
	}
	return u, true
}

// session loads a session and checks it belongs to the caller. A session that
// is not the caller's is reported as missing, not as forbidden, so an id cannot
// be probed.
func (s *Service) session(w http.ResponseWriter, r *http.Request) (UserIdentity, *Session, bool) {
	u, ok := s.user(w, r)
	if !ok {
		return u, nil, false
	}
	id := r.PathValue("id")
	sess, err := s.deps.Store.Get(r.Context(), id)
	if err != nil || !sess.OwnedBy(u) {
		s.fail(w, r, http.StatusNotFound, ErrNotFound)
		return u, nil, false
	}
	return u, sess, true
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("could not read the request: %w", err)
	}
	return nil
}

// safeFilename strips anything that could escape a directory or confuse a
// browser's download handling, and keeps the result short.
func safeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.' || r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.TrimLeft(b.String(), ".")
	if out == "" {
		out = "board.kicad_pcb"
	}
	if len(out) > 120 {
		out = out[:120]
	}
	return out
}
