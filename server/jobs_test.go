package server

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/embeddedci-com/pcb-autorouter/board"
	"github.com/embeddedci-com/pcb-autorouter/route"
)

// One router run at a time: a second one waits, and is told the server is
// busy when the first does not finish in time.
func TestASecondRouterRunIsToldTheServerIsBusy(t *testing.T) {
	h := newHarness(t)
	h.svc.deps.JobWait = 50 * time.Millisecond
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID

	started, finish := make(chan struct{}), make(chan struct{})
	withRouter(t, func(b *board.Board, p *board.Project, reqs []route.Request, o route.Options) ([]*route.Result, error) {
		close(started)
		<-finish
		return refused(b, p, reqs, o)
	})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/route", nil)
	}()
	<-started

	rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/route", nil)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" ||
		!strings.Contains(rec.Body.String(), "busy") {
		t.Errorf("second run: %d %q", rec.Code, rec.Body.String())
	}
	close(finish)
	wg.Wait()
}

// A router run that goes on too long is stopped, and the answer says so.
func TestARouterRunIsStoppedWhenItRunsTooLong(t *testing.T) {
	h := newHarness(t)
	h.svc.deps.RouteTimeout = 50 * time.Millisecond
	id := decode[SessionResponse](t, h.upload(true, true)).Session.ID

	withRouter(t, func(b *board.Board, p *board.Project, reqs []route.Request, o route.Options) ([]*route.Result, error) {
		select {
		case <-o.Stop:
		case <-time.After(5 * time.Second):
			t.Error("the router was never told to stop")
		}
		res, _ := refused(b, p, reqs, o)
		return res, route.ErrStopped
	})

	rec := h.postJSON("/api/pcb-trace-length-analyzer/sessions/"+id+"/route", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, truncate(rec.Body.String()))
	}
	got := decode[RouteResponse](t, rec)
	found := false
	for _, n := range got.Notes {
		if strings.Contains(n, "stopped after") {
			found = true
		}
	}
	if !found {
		t.Errorf("no note that the router stopped: %v", got.Notes)
	}
}
