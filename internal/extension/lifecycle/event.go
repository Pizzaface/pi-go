package lifecycle

import (
	"errors"
	"fmt"

	"github.com/pizzaface/go-pi/internal/extension/host"
)

// View is the read projection of a single extension. Services snapshot
// a view per call so callers don't need to hold locks while rendering.
type View struct {
	ID        string
	Mode      string // "compiled-in" | "hosted-go" | "hosted-ts"
	Trust     host.TrustClass
	State     host.State
	Version   string
	WorkDir   string
	Requested []string
	Granted   []string
	Approved  bool
	Err       string
}

// EventKind enumerates the kinds of state changes a Service emits.
type EventKind int

const (
	EventStateChanged EventKind = iota
	EventApprovalChanged
	EventRegistrationAdded
	EventRegistrationRemoved
)

func (k EventKind) String() string {
	switch k {
	case EventStateChanged:
		return "state_changed"
	case EventApprovalChanged:
		return "approval_changed"
	case EventRegistrationAdded:
		return "registration_added"
	case EventRegistrationRemoved:
		return "registration_removed"
	default:
		return "unknown"
	}
}

// Event is a single state-change notification. View is the post-change
// snapshot.
type Event struct {
	Kind EventKind
	View View
}

// Error is the canonical error shape returned by mutating Service
// methods. Op is the method name ("approve", "deny", etc.), ID is the
// extension id (may be empty on cross-cutting errors).
type Error struct {
	Op  string
	ID  string
	Err error
}

func (e *Error) Error() string {
	if e.ID == "" {
		return fmt.Sprintf("lifecycle: %s: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("lifecycle: %s %s: %v", e.Op, e.ID, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// Sentinel errors.
var (
	ErrCompiledIn       = errors.New("compiled-in extensions cannot be approved/denied/revoked/started/stopped")
	ErrUnknownExtension = errors.New("unknown extension")
)
