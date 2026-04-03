package daemon

import (
	"time"
)

const delegationHeartbeatTimeout = 30 * time.Second

// timeNow is a package-level var so tests can override it.
var timeNow = time.Now

// wardenTick checks all delegated vassals and releases any whose heartbeat
// has gone stale (older than timeout). Exported for testing.
func (d *Daemon) wardenTick(timeout time.Duration) {
	d.delegationMu.Lock()
	stale := make([]string, 0)
	for vassal, info := range d.delegatedVassals {
		if timeNow().Sub(info.LastHeartbeat) > timeout {
			stale = append(stale, vassal)
		}
	}
	for _, vassal := range stale {
		info := d.delegatedVassals[vassal]
		delete(d.delegatedVassals, vassal)
		d.logger.Warn("DELEGATION_EXPIRED",
			"vassal", vassal,
			"session_pid", info.SessionPID,
			"stale_for", timeNow().Sub(info.LastHeartbeat).Round(time.Second).String(),
		)
	}
	d.delegationMu.Unlock()

	// After releasing lock, check if orphaned vassals need restart.
	for _, vassal := range stale {
		d.maybeRestartOrphanedVassal(vassal)
	}
}

// maybeRestartOrphanedVassal checks if a formerly-delegated vassal's process
// is still alive. If dead, it logs a warning (actual restart is handled by
// the existing watchVassal goroutine which will see isDelegated==false).
func (d *Daemon) maybeRestartOrphanedVassal(vassal string) {
	d.vassalProcsMu.RLock()
	proc, ok := d.vassalProcs[vassal]
	d.vassalProcsMu.RUnlock()

	if !ok {
		// Not a claude-type vassal tracked here — nothing to do.
		return
	}

	if err := proc.process.Signal(nil); err != nil {
		// Process is dead — log warning so operator knows.
		d.logger.Warn("DELEGATION_ORPHAN_DEAD",
			"vassal", vassal,
			"msg", "vassal process died during delegation; daemon will restart via watchVassal",
		)
	}
	// If alive, watchVassal will pick it up normally since isDelegated is now false.
}

// runDelegationWarden runs a background loop checking for stale delegations.
// Started by daemon.Start().
func (d *Daemon) runDelegationWarden() {
	defer d.wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.wardenTick(delegationHeartbeatTimeout)
		}
	}
}
