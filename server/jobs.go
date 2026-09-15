package server

import (
	"errors"
	"net/http"
	"time"
)

// Heavy jobs share the process with every other user of the host, so only a
// few run at once. A router run can hold hundreds of MB for minutes; on a 4 GB
// host, a handful started together took the whole machine down. A request
// that finds no free slot waits a little, then is told the server is busy
// rather than being queued without end.

type jobKind int

const (
	routeJob jobKind = iota
	heavyJob
)

type jobSlots struct {
	route, heavy chan struct{}
}

func newJobSlots(d *Deps) *jobSlots {
	r, h := d.MaxRouteJobs, d.MaxHeavyJobs
	if r <= 0 {
		r = DefaultMaxRouteJobs
	}
	if h <= 0 {
		h = DefaultMaxHeavyJobs
	}
	return &jobSlots{route: make(chan struct{}, r), heavy: make(chan struct{}, h)}
}

var errBusy = errors.New("the server is busy with other boards right now; try again in a minute")

// job takes a slot of the given kind for the request, and returns the release.
// When none frees up in time it answers 503 itself and reports false; when the
// caller goes away while waiting it answers nothing.
func (s *Service) job(w http.ResponseWriter, r *http.Request, kind jobKind) (func(), bool) {
	slots := s.jobs.heavy
	if kind == routeJob {
		slots = s.jobs.route
	}
	wait := time.NewTimer(s.deps.jobWait())
	defer wait.Stop()
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, true
	case <-r.Context().Done():
		return nil, false
	case <-wait.C:
		w.Header().Set("Retry-After", "60")
		s.fail(w, r, http.StatusServiceUnavailable, errBusy)
		return nil, false
	}
}
