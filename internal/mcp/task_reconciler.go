package mcp

import (
	"context"
	"encoding/json"
	"time"
)

// submitTaskToVassal dispatches a task to a vassal and updates the gateway task
// record with the vassal's response. Called asynchronously from handleDispatchTask.
func (s *Server) submitTaskToVassal(gtID, vassalName, task, contextJSON string) {
	if s.vassalPool == nil {
		s.taskStore.UpdateGatewayTask(gtID, "failed", "", "", "vassal pool not available")
		return
	}

	client, ok := s.vassalPool.Get(vassalName)
	if !ok {
		s.taskStore.UpdateGatewayTask(gtID, "failed", "", "", "vassal not found: "+vassalName)
		return
	}

	args := map[string]any{"task": task}
	if contextJSON != "" {
		var ctxObj any
		if json.Unmarshal([]byte(contextJSON), &ctxObj) == nil {
			args["context"] = ctxObj
		}
	}

	callCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := client.CallTool(callCtx, "dispatch_task", args)
	if err != nil {
		errMsg := err.Error()
		if callCtx.Err() == context.DeadlineExceeded {
			errMsg = "vassal dispatch_task timeout (30s)"
		}
		s.taskStore.UpdateGatewayTask(gtID, "failed", "", "", errMsg)
		return
	}

	// Parse vassal response to extract vassal_task_id.
	var resp map[string]any
	vassalTaskID := ""
	if json.Unmarshal([]byte(result), &resp) == nil {
		if tid, ok := resp["task_id"].(string); ok {
			vassalTaskID = tid
		}
	}

	s.taskStore.UpdateGatewayTask(gtID, "accepted", vassalTaskID, "", "")
}

// abortTaskOnVassal sends a best-effort abort_task call to a vassal.
func (s *Server) abortTaskOnVassal(vassalName, vassalTaskID string) {
	if s.vassalPool == nil {
		return
	}
	client, ok := s.vassalPool.Get(vassalName)
	if !ok {
		return
	}

	callCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := client.CallTool(callCtx, "abort_task", map[string]any{
		"task_id": vassalTaskID,
	})
	if err != nil {
		s.logger.Warn("best-effort abort_task failed", "vassal", vassalName, "task_id", vassalTaskID, "err", err)
	}
}

// StartTaskReconciler runs a background loop that periodically polls vassals
// for the status of active gateway tasks and updates the local DB cache.
func (s *Server) StartTaskReconciler(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileTasks(ctx)
		}
	}
}

func (s *Server) reconcileTasks(ctx context.Context) {
	if s.taskStore == nil || s.vassalPool == nil {
		return
	}

	tasks, err := s.taskStore.ListActiveGatewayTasks()
	if err != nil {
		s.logger.Warn("reconciler: failed to list active tasks", "err", err)
		return
	}

	for _, gt := range tasks {
		if ctx.Err() != nil {
			return
		}

		// Skip tasks without a vassal_task_id — they're still being submitted.
		if gt.VassalTaskID == "" {
			// If queued for >5 min without vassal_task_id, mark as failed.
			if created, err := time.Parse("2006-01-02 15:04:05", gt.CreatedAt); err == nil {
				if time.Since(created) > 5*time.Minute {
					s.taskStore.UpdateGatewayTask(gt.TaskID, "failed", "", "", "submission timeout: vassal never responded")
				}
			}
			continue
		}

		client, ok := s.vassalPool.Get(gt.VassalName)
		if !ok {
			// Vassal disconnected — if >5 min since last update, mark failed.
			if updated, err := time.Parse("2006-01-02 15:04:05", gt.UpdatedAt); err == nil {
				if time.Since(updated) > 5*time.Minute {
					s.taskStore.UpdateGatewayTask(gt.TaskID, "failed", "", "", "vassal unreachable")
				}
			}
			continue
		}

		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		result, err := client.CallTool(callCtx, "get_task_status", map[string]any{
			"task_id": gt.VassalTaskID,
		})
		cancel()

		if err != nil {
			s.logger.Debug("reconciler: get_task_status failed", "vassal", gt.VassalName, "task_id", gt.VassalTaskID, "err", err)
			continue
		}

		var resp map[string]any
		if json.Unmarshal([]byte(result), &resp) != nil {
			continue
		}

		status, _ := resp["status"].(string)
		if status == "" {
			continue
		}

		// Map vassal statuses to gateway statuses.
		newStatus := status
		switch status {
		case "pending", "in_progress":
			newStatus = "running"
		case "completed":
			newStatus = "done"
		}

		taskResult := ""
		if r, ok := resp["output"].(string); ok {
			taskResult = r
		} else if r, ok := resp["result"].(string); ok {
			taskResult = r
		}
		errMsg := ""
		if e, ok := resp["error"].(string); ok {
			errMsg = e
		}

		if newStatus != gt.Status || taskResult != "" || errMsg != "" {
			s.taskStore.UpdateGatewayTask(gt.TaskID, newStatus, "", taskResult, errMsg)
		}
	}
}
