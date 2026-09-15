package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/embeddedci-com/pcb-autorouter/board"
)

// Session is one uploaded board and what has been decided about it.
//
// The parameters live here rather than being re-sent with every request
// because they are what the user is iterating on: upload once, adjust the
// clock offset and the tolerances, look at the plan again, and only then
// apply. Keeping them server-side also means the board that gets tuned is the
// board that was analysed, which a stateless design could not promise.
type Session struct {
	ID string `json:"id"`

	// UserID and OrganizationID are the identity the session was filed under.
	// Every read checks them: a session id is not an access token.
	UserID         string `json:"user_id"`
	OrganizationID string `json:"organization_id"`

	// Filename is what the user uploaded, for display.
	Filename string `json:"filename"`

	// BoardBytes is the uploaded size.
	BoardBytes int `json:"board_bytes"`

	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`

	// Params are the current matching parameters.
	Params Params `json:"params"`

	// Project is the net class information the board was read with, kept in
	// full rather than re-derived.
	//
	// Without it every clearance would fall back to the board minimum on the
	// next request, and the meanders applied would be checked against looser
	// rules than the ones the user was shown. It is also the record of whether
	// a .kicad_pro was supplied at all, which the report states plainly
	// because the answer changes what the clearance check means.
	Project *board.Project `json:"project"`

	// HasProjectFile records whether the user actually uploaded one.
	HasProjectFile bool `json:"has_project_file"`

	// Applied records the last apply, or nil if nothing has been applied.
	Applied *ApplyRecord `json:"applied,omitempty"`

	// Routed records the last run of the router, or nil if it has not been
	// run. When it is set, the board in this session is the routed one: the
	// router writes new copper, and a plan measured against the copper that
	// was there before would be matching a topology the board no longer has.
	Routed *RouteRecord `json:"routed,omitempty"`
}

// RouteRecord is what the router made of the missing fly-by hops.
type RouteRecord struct {
	At time.Time `json:"at"`

	// Requested is how many connections were missing, Connected how many were
	// made. They are rarely equal, and the difference is the point: a hop that
	// could not be made is a placement decision the tool is not entitled to
	// take.
	Requested int `json:"requested"`
	Connected int `json:"connected"`

	AddedMM float64 `json:"added_mm"`
	Vias    int     `json:"vias"`

	ResultFilename string `json:"result_filename"`
	ResultBytes    int    `json:"result_bytes"`
}

// Expired reports whether the session has outlived its TTL.
func (s *Session) Expired(now time.Time) bool { return !now.Before(s.ExpiresAt) }

// OwnedBy reports whether an identity may see this session.
func (s *Session) OwnedBy(u UserIdentity) bool {
	if u.UserID == "" {
		return false
	}
	return s.UserID == u.UserID
}

// ApplyRecord is the outcome of a tuning run, kept so the result page survives
// a page reload.
type ApplyRecord struct {
	At time.Time `json:"at"`

	// Nets is what was asked to change.
	Nets []string `json:"nets"`

	AddedMM     float64 `json:"added_mm"`
	ShortfallMM float64 `json:"shortfall_mm"`
	NetsMet     int     `json:"nets_met"`
	NetsShort   int     `json:"nets_short"`

	// ResultFilename is what the download is offered as.
	ResultFilename string `json:"result_filename"`
	ResultBytes    int    `json:"result_bytes"`
}

// newID makes an unguessable session id. A session id is not an access token --
// every read also checks the owner -- but an id that could be guessed would
// still let one user learn that another's board exists.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("pcb-trace-length-analyzer: cannot read randomness for a session id: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// MemoryStore keeps sessions in this process.
//
// It is the right store for the standalone harness and for tests, and the
// wrong one behind a load balancer: a second instance would not find the
// board. A host running more than one instance supplies its own, which is why
// Store is an interface.
type MemoryStore struct {
	mu   sync.Mutex
	now  func() time.Time
	rows map[string]*memRow

	// maxBytes bounds the boards held altogether, zero meaning no bound.
	maxBytes int64
}

// SetMaxBytes bounds how much board data the store holds across every session.
//
// Per-user limits do not bound a public tool: every browser that has never
// signed in is a user of its own, so a hundred visitors are a hundred quotas.
// This is the one number that keeps the process inside the machine it runs
// on. When a new board would take the total past it, the oldest sessions are
// dropped first -- they are hours old and expire on their own anyway, and a
// new visitor being told "the server is full" would be the worse outcome.
func (m *MemoryStore) SetMaxBytes(n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxBytes = n
}

func (r *memRow) bytes() int64 { return int64(len(r.board) + len(r.result)) }

// makeRoomLocked evicts the oldest sessions until adding extra bytes fits.
func (m *MemoryStore) makeRoomLocked(extra int64, keep string) {
	if m.maxBytes <= 0 {
		return
	}
	var total int64
	for _, r := range m.rows {
		total += r.bytes()
	}
	for total+extra > m.maxBytes {
		oldest := ""
		for id, r := range m.rows {
			if id == keep {
				continue
			}
			if oldest == "" || r.s.CreatedAt.Before(m.rows[oldest].s.CreatedAt) {
				oldest = id
			}
		}
		if oldest == "" {
			return
		}
		total -= m.rows[oldest].bytes()
		delete(m.rows, oldest)
	}
}

type memRow struct {
	s      Session
	board  []byte
	result []byte
}

// NewMemoryStore builds an in-process store. now may be nil.
func NewMemoryStore(now func() time.Time) *MemoryStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemoryStore{now: now, rows: map[string]*memRow{}}
}

func (m *MemoryStore) Put(_ context.Context, s *Session, board []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked()
	delete(m.rows, s.ID)
	m.makeRoomLocked(int64(len(board)), s.ID)
	cp := make([]byte, len(board))
	copy(cp, board)
	m.rows[s.ID] = &memRow{s: *s, board: cp}
	return nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok || r.s.Expired(m.now()) {
		return nil, ErrNotFound
	}
	out := r.s
	return &out, nil
}

func (m *MemoryStore) Board(_ context.Context, id string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok || r.s.Expired(m.now()) {
		return nil, ErrNotFound
	}
	return r.board, nil
}

func (m *MemoryStore) PutResult(_ context.Context, id string, board []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok || r.s.Expired(m.now()) {
		return ErrNotFound
	}
	r.result = nil
	m.makeRoomLocked(int64(len(board)), id)
	cp := make([]byte, len(board))
	copy(cp, board)
	r.result = cp
	return nil
}

func (m *MemoryStore) Result(_ context.Context, id string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok || r.s.Expired(m.now()) || r.result == nil {
		return nil, ErrNotFound
	}
	return r.result, nil
}

func (m *MemoryStore) Update(_ context.Context, s *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[s.ID]
	if !ok || r.s.Expired(m.now()) {
		return ErrNotFound
	}
	r.s = *s
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, id)
	return nil
}

func (m *MemoryStore) ListByUser(_ context.Context, userID string, limit int) ([]*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked()
	var out []*Session
	for _, r := range m.rows {
		if r.s.UserID != userID {
			continue
		}
		cp := r.s
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// sweepLocked drops expired rows, so an abandoned board does not hold onto
// megabytes for the life of the process.
func (m *MemoryStore) sweepLocked() {
	now := m.now()
	for id, r := range m.rows {
		if r.s.Expired(now) {
			delete(m.rows, id)
		}
	}
}

// Len reports how many sessions are held, for tests and diagnostics.
func (m *MemoryStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}
