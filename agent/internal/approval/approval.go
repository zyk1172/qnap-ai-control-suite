// Package approval owns short-lived, single-use confirmations for sensitive
// operations.  It intentionally stores tickets in memory: a restart revokes
// every outstanding approval rather than accidentally replaying a prior one.
package approval

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"qnap-ai-control-suite/agent/internal/operations"
)

type State string

const (
	Pending  State = "pending"
	Approved State = "approved"
	Denied   State = "denied"
	Used     State = "used"
)

var (
	ErrNotFound        = errors.New("approval not found")
	ErrExpired         = errors.New("approval expired")
	ErrNotApproved     = errors.New("approval has not been approved")
	ErrDenied          = errors.New("approval was denied")
	ErrUsed            = errors.New("approval was already used")
	ErrBindingMismatch = errors.New("approval does not match this operation")
)

type Binding struct {
	Method      string
	Path        string
	RequestHash string
	Operation   string
}

type Ticket struct {
	ID         string          `json:"approval_id"`
	RequestID  string          `json:"request_id"`
	Operation  string          `json:"operation"`
	Risk       operations.Risk `json:"risk"`
	Target     string          `json:"target,omitempty"`
	Summary    string          `json:"summary"`
	State      State           `json:"state"`
	CreatedAt  time.Time       `json:"created_at"`
	DecisionAt *time.Time      `json:"decision_at,omitempty"`
	ExpiresAt  time.Time       `json:"expires_at"`
	binding    Binding
}

type Options struct {
	TTL time.Duration
	Now func() time.Time
	ID  func() string
}

type Manager struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	newID   func() string
	tickets map[string]*Ticket
}

func New(ttl time.Duration) *Manager { return NewWithOptions(Options{TTL: ttl}) }

func NewWithOptions(options Options) *Manager {
	if options.TTL <= 0 {
		options.TTL = 10 * time.Minute
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.ID == nil {
		options.ID = randomID
	}
	return &Manager{ttl: options.TTL, now: options.Now, newID: options.ID, tickets: map[string]*Ticket{}}
}

func (m *Manager) Create(requestID string, operation operations.Operation, binding Binding) Ticket {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked()
	now := m.now()
	ticket := &Ticket{ID: m.newID(), RequestID: requestID, Operation: operation.Name, Risk: operation.Risk, Target: operation.Target, Summary: operation.Summary, State: Pending, CreatedAt: now, ExpiresAt: now.Add(m.ttl), binding: binding}
	m.tickets[ticket.ID] = ticket
	return clone(*ticket)
}

func (m *Manager) Decide(id string, approve bool) (Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ticket, err := m.activeLocked(id)
	if err != nil {
		return Ticket{}, err
	}
	if ticket.State == Used {
		return Ticket{}, ErrUsed
	}
	if ticket.State == Denied {
		return Ticket{}, ErrDenied
	}
	if approve {
		ticket.State = Approved
	} else {
		ticket.State = Denied
	}
	decisionAt := m.now()
	ticket.DecisionAt = &decisionAt
	return clone(*ticket), nil
}

// Lookup returns a redacted ticket snapshot for audit correlation. It never
// changes the ticket state and does not authorize execution.
func (m *Manager) Lookup(id string) (Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ticket, err := m.activeLocked(id)
	if err != nil {
		return Ticket{}, err
	}
	return clone(*ticket), nil
}

func (m *Manager) Consume(id string, binding Binding) (Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ticket, err := m.activeLocked(id)
	if err != nil {
		return Ticket{}, err
	}
	if ticket.binding != binding {
		return Ticket{}, ErrBindingMismatch
	}
	switch ticket.State {
	case Pending:
		return Ticket{}, ErrNotApproved
	case Approved:
		ticket.State = Used
		return clone(*ticket), nil
	case Denied:
		return Ticket{}, ErrDenied
	case Used:
		return Ticket{}, ErrUsed
	default:
		return Ticket{}, ErrNotApproved
	}
}

func (m *Manager) activeLocked(id string) (*Ticket, error) {
	ticket, ok := m.tickets[id]
	if !ok {
		return nil, ErrNotFound
	}
	if !m.now().Before(ticket.ExpiresAt) {
		delete(m.tickets, id)
		return nil, ErrExpired
	}
	return ticket, nil
}

func (m *Manager) pruneLocked() {
	now := m.now()
	for id, ticket := range m.tickets {
		if !now.Before(ticket.ExpiresAt) {
			delete(m.tickets, id)
		}
	}
}

func BindingFor(method, path string, body []byte, operation string) Binding {
	sum := sha256.Sum256(body)
	return Binding{Method: method, Path: path, RequestHash: hex.EncodeToString(sum[:]), Operation: operation}
}

func clone(ticket Ticket) Ticket { ticket.binding = Binding{}; return ticket }

func randomID() string {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(b)
}
