// Package server is the HTTP control plane for the PCB trace length analyzer.
//
// It mounts into a host application with one call and depends on nothing
// concrete: everything it needs from the host -- where to keep an uploaded
// board, who is asking, what time it is -- lives behind the interfaces in this
// file. embeddedci-server satisfies them with its own object storage and its
// own authentication middleware; the standalone harness in
// cmd/pcb-trace-length-analyzer-server satisfies them with a map and a temporary
// directory. Neither codebase needs to know anything about the other.
//
// Unlike the EMI analyzer, there is no worker and no queue here. The routing
// engine is Go and runs in process: reading a 2.4 MB board takes about 40 ms
// and analysing its whole DDR interface about 100 ms, so a request answers
// directly rather than scheduling anything. That does mean board bytes pass
// through this package, which is why there is an upload limit and why sessions
// expire.
package server

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

var (
	// ErrNotFound means the session does not exist, has expired, or belongs to
	// somebody else. Deliberately one error for all three, so a caller cannot
	// probe for other people's session ids.
	ErrNotFound = errors.New("pcb-trace-length-analyzer: not found")

	// ErrTooLarge means the upload exceeded MaxUploadBytes.
	ErrTooLarge = errors.New("pcb-trace-length-analyzer: upload too large")

	// ErrBadBoard means the file was not a readable .kicad_pcb.
	ErrBadBoard = errors.New("pcb-trace-length-analyzer: not a KiCad board")
)

// Store keeps uploaded boards and the state of a session between requests.
//
// A session has to outlive a single request: the user uploads a board, reads
// the report, chooses parameters, and only then applies. The host owns where
// that lives because it is the host that knows whether it is running on one
// machine or several -- an in-memory map is correct for the dev harness and
// wrong behind a load balancer.
type Store interface {
	// Put saves a session and its board bytes.
	//
	// It is also how a session's board is replaced, which happens when the
	// router writes new copper: from then on the session works on the routed
	// board. An implementation may drop anything else it holds for the session
	// when this is called -- the caller re-saves what still applies.
	Put(ctx context.Context, s *Session, board []byte) error

	// Get returns a session. It must return ErrNotFound for an expired one.
	Get(ctx context.Context, id string) (*Session, error)

	// Board returns the uploaded board bytes for a session.
	Board(ctx context.Context, id string) ([]byte, error)

	// PutResult saves the tuned board for later download.
	PutResult(ctx context.Context, id string, board []byte) error

	// Result returns the tuned board, or ErrNotFound if nothing was applied.
	Result(ctx context.Context, id string) ([]byte, error)

	// Update replaces a session's metadata, leaving its boards alone.
	Update(ctx context.Context, s *Session) error

	// Delete removes a session and everything belonging to it.
	Delete(ctx context.Context, id string) error

	// ListByUser returns a user's sessions, newest first.
	ListByUser(ctx context.Context, userID string, limit int) ([]*Session, error)
}

// UserIdentity is the authenticated human behind a request. The host supplies
// it; in embeddedci-server it comes from the existing JWT middleware.
type UserIdentity struct {
	UserID         string
	OrganizationID string

	// Login is a human-readable name, when the host has one.
	Login string

	// Anonymous marks a request that carried no credential. The host decides
	// whether to allow those at all; this package only reports it, and refuses
	// to file a session under an empty user.
	Anonymous bool
}

type ctxKey int

const ctxKeyUser ctxKey = iota

// WithUser attaches an identity to a request context. Hosts call this from
// their own auth middleware before delegating to these handlers.
func WithUser(ctx context.Context, u UserIdentity) context.Context {
	return context.WithValue(ctx, ctxKeyUser, u)
}

// UserFrom returns the identity a host attached with WithUser.
func UserFrom(ctx context.Context) (UserIdentity, bool) {
	u, ok := ctx.Value(ctxKeyUser).(UserIdentity)
	return u, ok
}

// Deps is the wiring a host passes to New.
type Deps struct {
	Store Store

	// Now is injectable so tests do not have to wait for a session to expire.
	Now func() time.Time

	Logger *slog.Logger

	// SessionTTL is how long an uploaded board is kept. Zero means
	// DefaultSessionTTL.
	SessionTTL time.Duration

	// MaxUploadBytes caps an upload. Zero means DefaultMaxUploadBytes.
	MaxUploadBytes int64

	// MaxSessionsPerUser caps how many boards one user may have in flight.
	// Zero means DefaultMaxSessionsPerUser.
	MaxSessionsPerUser int
}

// Defaults for the knobs on Deps.
//
// The TTL is long enough to read a report, think about the tolerances and come
// back to it, and short enough that a board nobody is working on does not sit
// in storage for days. The upload limit is generous against the 2.4 MB demo
// board -- a large design with many layers can be several times that -- while
// still bounding what one request can ask this process to hold.
const (
	DefaultSessionTTL         = 4 * time.Hour
	DefaultMaxUploadBytes     = 64 << 20
	DefaultMaxSessionsPerUser = 20
)

func (d *Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now().UTC()
}

func (d *Deps) log() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}

func (d *Deps) sessionTTL() time.Duration {
	if d.SessionTTL > 0 {
		return d.SessionTTL
	}
	return DefaultSessionTTL
}

func (d *Deps) maxUpload() int64 {
	if d.MaxUploadBytes > 0 {
		return d.MaxUploadBytes
	}
	return DefaultMaxUploadBytes
}

func (d *Deps) maxSessions() int {
	if d.MaxSessionsPerUser > 0 {
		return d.MaxSessionsPerUser
	}
	return DefaultMaxSessionsPerUser
}
