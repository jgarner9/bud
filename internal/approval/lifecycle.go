// Package approval defines the transitions of an approval request.
// It does not authenticate callers, enforce policy, or execute actions.
package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

type State string

const (
	Pending  State = "pending"
	Approved State = "approved"
	Denied   State = "denied"
	Canceled State = "canceled"
	Expired  State = "expired"
	Executed State = "executed"
)

var ErrTransition = errors.New("invalid approval transition")

// Request binds an approval to a session, target and digest of exact arguments.
type Request struct {
	ID            string    `json:"id"`
	Session       string    `json:"session"`
	Action        string    `json:"action"`
	Target        string    `json:"target"`
	ArgumentsHash string    `json:"arguments_hash"`
	State         State     `json:"state"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// New creates a pending request with the default one-hour expiry.
func New(id, session, action, target string, arguments []byte, now time.Time) (Request, error) {
	if id == "" || session == "" || action == "" || target == "" || now.IsZero() {
		return Request{}, errors.New("approval requires an ID, session, action, target and time")
	}
	sum := sha256.Sum256(arguments)
	return Request{ID: id, Session: session, Action: action, Target: target, ArgumentsHash: hex.EncodeToString(sum[:]), State: Pending, ExpiresAt: now.Add(time.Hour)}, nil
}

// Decide records a response or cancellation. An expired pending request is
// transitioned to Expired before any decision is considered.
func (r *Request) Decide(next State, now time.Time) error {
	if r.State != Pending || now.IsZero() {
		return ErrTransition
	}
	if !now.Before(r.ExpiresAt) {
		r.State = Expired
		return ErrTransition
	}
	if next != Approved && next != Denied && next != Canceled {
		return ErrTransition
	}
	r.State = next
	return nil
}

// Consume marks an approved request executed when its complete binding matches.
// The caller must persist this transition atomically before executing an action.
func (r *Request) Consume(session, action, target string, arguments []byte, now time.Time) error {
	if r.State != Approved || now.IsZero() {
		return ErrTransition
	}
	if !now.Before(r.ExpiresAt) {
		r.State = Expired
		return ErrTransition
	}
	sum := sha256.Sum256(arguments)
	if r.Session != session || r.Action != action || r.Target != target || r.ArgumentsHash != hex.EncodeToString(sum[:]) {
		return ErrTransition
	}
	r.State = Executed
	return nil
}
