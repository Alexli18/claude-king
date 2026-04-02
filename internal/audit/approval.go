package audit

import (
	"fmt"
	"sync"
)

// ApprovalManager manages pending Sovereign Approval requests.
// exec_in callers block on the channel returned by Request; the channel is
// unblocked by a matching Respond call (from respond_approval RPC/MCP).
type ApprovalManager struct {
	mu      sync.Mutex
	pending map[string]chan bool // requestID → response channel
}

// NewApprovalManager creates a new ApprovalManager.
func NewApprovalManager() *ApprovalManager {
	return &ApprovalManager{
		pending: make(map[string]chan bool),
	}
}

// Request registers a pending approval and returns a channel to receive on.
// The channel is buffered (capacity 1) so Respond never blocks.
func (am *ApprovalManager) Request(requestID string) <-chan bool {
	ch := make(chan bool, 1)
	am.mu.Lock()
	am.pending[requestID] = ch
	am.mu.Unlock()
	return ch
}

// Respond delivers the approval decision to a waiting exec_in caller.
// Returns an error if no pending request with the given ID exists.
func (am *ApprovalManager) Respond(requestID string, approved bool) error {
	am.mu.Lock()
	ch, ok := am.pending[requestID]
	if ok {
		delete(am.pending, requestID)
	}
	am.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending approval for request %s", requestID)
	}
	ch <- approved
	return nil
}

// Cancel removes a pending request without delivering a response.
// Returns true if the entry existed (i.e., it was not already responded to).
// Used when an approval times out: only write "timeout" to the DB if Cancel returns true.
func (am *ApprovalManager) Cancel(requestID string) bool {
	am.mu.Lock()
	_, ok := am.pending[requestID]
	delete(am.pending, requestID)
	am.mu.Unlock()
	return ok
}
