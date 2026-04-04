package daemon

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"time"

	"github.com/alexli18/claude-king/internal/webhook"
)

// ansiStrip removes ANSI escape sequences so log_watch patterns match
// coloured terminal output correctly.
var ansiStrip = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[=>]|\x1b\(B|\x07|\r|\x08.?`)

// startGuardRunners spawns one goroutine per guard entry across all configured
// vassals. Each goroutine runs an independent ticker loop that checks health and
// updates the circuit breaker state. Safe to call after config is loaded.
func (d *Daemon) startGuardRunners(ctx context.Context) {
	for _, vc := range d.config.Vassals {
		for i, gc := range vc.Guards {
			key := fmt.Sprintf("%s:%d", vc.Name, i)

			// Pre-populate state entry so guard_status works immediately.
			d.guardStatesMu.Lock()
			d.guardStates[key] = &GuardState{
				VassalName: vc.Name,
				GuardIndex: i,
				GuardType:  gc.Type,
			}
			d.guardStatesMu.Unlock()

			interval := gc.Interval
			if interval <= 0 {
				interval = 10
			}
			threshold := gc.Threshold
			if threshold <= 0 {
				threshold = 3
			}

			vassalName := vc.Name
			guardIdx := i
			guardCfg := gc
			guardKey := key

			d.wg.Add(1)
			go func() {
				defer d.wg.Done()
				ticker := time.NewTicker(time.Duration(interval) * time.Second)
				defer ticker.Stop()

				// prevBytes tracks the byte count from the previous tick for data_rate.
				var prevBytes int64
				firstDataTick := true

				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						var result GuardResult
						switch guardCfg.Type {
						case "port_check":
							result = checkPortOpen(guardCfg.Port, guardCfg.Expect)

						case "log_watch":
							result = d.checkLogWatch(vassalName, guardCfg.CompiledPatterns, time.Duration(interval)*time.Second)

						case "data_rate":
							var curBytes int64
							if d.ptyMgr != nil {
								curBytes = d.ptyMgr.GetSessionBytesWritten(vassalName)
							}
							if firstDataTick {
								prevBytes = curBytes
								firstDataTick = false
								result = GuardResult{OK: true, Message: "initial sample — no rate yet", CheckedAt: time.Now()}
							} else {
								result = checkDataRate(curBytes, prevBytes, guardCfg.MinBytesPerSec, interval)
								prevBytes = curBytes
							}

						case "health_check":
							timeout := guardCfg.Timeout
							if timeout <= 0 {
								timeout = 10
							}
							result = checkHealthScript(guardCfg.Exec, time.Duration(timeout)*time.Second, d.rootDir)
						}

						d.updateGuardState(guardKey, result, threshold)
					}
				}
			}()

			d.logger.Info("guard runner started",
				"vassal", vassalName,
				"guard_index", guardIdx,
				"type", guardCfg.Type,
				"interval_s", interval,
				"threshold", threshold,
			)
		}
	}
}

// checkPortOpen verifies whether a TCP port is in the expected state (open or closed).
func checkPortOpen(port int, expect string) GuardResult {
	address := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	now := time.Now()

	expectOpen := expect != "closed" // default is "open"
	if err == nil {
		conn.Close()
		if expectOpen {
			return GuardResult{OK: true, Message: fmt.Sprintf("port %d is open (OK)", port), CheckedAt: now}
		}
		return GuardResult{OK: false, Message: fmt.Sprintf("port %d is open but expected closed", port), CheckedAt: now}
	}
	if expectOpen {
		return GuardResult{OK: false, Message: fmt.Sprintf("port %d is not reachable: %v", port, err), CheckedAt: now}
	}
	return GuardResult{OK: true, Message: fmt.Sprintf("port %d is closed (OK)", port), CheckedAt: now}
}

// checkLogWatch scans recent PTY output lines for any configured error patterns.
func (d *Daemon) checkLogWatch(vassalName string, patterns []*regexp.Regexp, interval time.Duration) GuardResult {
	since := time.Now().Add(-interval)
	var lines []string
	if d.ptyMgr != nil {
		lines = d.ptyMgr.GetSessionRecentLines(vassalName, since)
	}
	now := time.Now()

	for _, line := range lines {
		clean := ansiStrip.ReplaceAllString(line, "")
		for _, pat := range patterns {
			if pat.MatchString(clean) {
				return GuardResult{OK: false, Message: fmt.Sprintf("error pattern %q matched in output: %q", pat.String(), clean), CheckedAt: now}
			}
		}
	}
	return GuardResult{OK: true, Message: "no error patterns found in recent output", CheckedAt: now}
}

// checkDataRate computes the byte throughput between two samples and checks it
// against the minimum threshold (in bytes/sec).
func checkDataRate(curBytes, prevBytes int64, minBytesPerSec float64, intervalSec int) GuardResult {
	now := time.Now()
	delta := curBytes - prevBytes
	if delta < 0 {
		delta = 0 // guard against counter reset
	}
	rate := float64(delta) / float64(intervalSec)
	if rate < minBytesPerSec {
		return GuardResult{
			OK:        false,
			Message:   fmt.Sprintf("data rate %.1f B/s is below minimum %.1f B/s", rate, minBytesPerSec),
			CheckedAt: now,
		}
	}
	return GuardResult{
		OK:        true,
		Message:   fmt.Sprintf("data rate %.1f B/s OK (min %.1f B/s)", rate, minBytesPerSec),
		CheckedAt: now,
	}
}

// checkHealthScript runs an external health-check script and returns success if
// it exits 0 within the given timeout.
func checkHealthScript(execPath string, timeout time.Duration, rootDir string) GuardResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, execPath)
	cmd.Dir = rootDir
	out, err := cmd.CombinedOutput()
	now := time.Now()

	if err == nil {
		return GuardResult{OK: true, Message: "health check passed", CheckedAt: now}
	}
	if ctx.Err() == context.DeadlineExceeded {
		return GuardResult{OK: false, Message: fmt.Sprintf("health check timed out after %v", timeout), CheckedAt: now}
	}
	msg := string(out)
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	return GuardResult{OK: false, Message: fmt.Sprintf("health check failed: %v; output: %s", err, msg), CheckedAt: now}
}

// updateGuardState applies a check result to the guard state, updating the
// consecutive failure counter and opening/closing the circuit breaker as needed.
func (d *Daemon) updateGuardState(key string, result GuardResult, threshold int) {
	d.guardStatesMu.Lock()
	defer d.guardStatesMu.Unlock()

	gs, ok := d.guardStates[key]
	if !ok {
		return
	}

	gs.LastCheckTime = result.CheckedAt
	gs.LastResult = result

	if result.OK {
		wasOpen := gs.CircuitOpen
		gs.ConsecutiveFails = 0
		gs.CircuitOpen = false
		if wasOpen {
			d.logger.Warn("GUARD_CIRCUIT_CLOSED",
				"vassal", gs.VassalName,
				"guard_type", gs.GuardType,
				"guard_index", gs.GuardIndex,
			)
			if d.webhookDispatcher != nil {
				d.webhookDispatcher.Send(webhook.Payload{
					Vassal:   gs.VassalName,
					Event:    "guard_circuit_closed",
					Severity: "info",
					Summary:  fmt.Sprintf("guard %s on vassal %s recovered", gs.GuardType, gs.VassalName),
				})
			}
		}
	} else {
		gs.ConsecutiveFails++
		if gs.ConsecutiveFails >= threshold && !gs.CircuitOpen {
			gs.CircuitOpen = true
			d.logger.Warn("GUARD_CIRCUIT_OPEN",
				"vassal", gs.VassalName,
				"guard_type", gs.GuardType,
				"guard_index", gs.GuardIndex,
				"fails", gs.ConsecutiveFails,
			)
			if d.webhookDispatcher != nil {
				d.webhookDispatcher.Send(webhook.Payload{
					Vassal:   gs.VassalName,
					Event:    "guard_circuit_open",
					Severity: "error",
					Summary:  fmt.Sprintf("guard %s on vassal %s opened after %d failures", gs.GuardType, gs.VassalName, gs.ConsecutiveFails),
				})
			}
		}
	}
}
