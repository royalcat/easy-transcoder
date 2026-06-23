package worker

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
)

// APIHandlers holds HTTP handlers for the worker API endpoints.
type APIHandlers struct {
	manager *Manager
	logger  *slog.Logger
}

// NewAPIHandlers creates handler functions for worker API endpoints.
func NewAPIHandlers(manager *Manager, logger *slog.Logger) *APIHandlers {
	return &APIHandlers{manager: manager, logger: logger}
}

// HandleRegister handles POST /api/v1/worker/register
func (h *APIHandlers) HandleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	worker, err := h.manager.Register(req)
	if err != nil {
		h.logger.Error("worker registration failed", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := RegisterResponse{
		WorkerID:          worker.ID,
		HeartbeatInterval: h.manager.config.HeartbeatInterval,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// HandleHeartbeat handles POST /api/v1/worker/heartbeat
func (h *APIHandlers) HandleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.manager.Heartbeat(req.WorkerID, req.CurrentTaskID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

// HandleAcquireTask handles POST /api/v1/worker/task/acquire
func (h *APIHandlers) HandleAcquireTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkerID string `json:"worker_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	task, err := h.manager.AcquireTask(req.WorkerID)
	if err != nil {
		h.logger.Error("task acquire failed", "worker_id", req.WorkerID, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if task == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

// HandleTaskInput handles GET /api/v1/worker/task/input/{taskID}
// Streams the input file bytes to the worker for pipe-based transcoding.
func (h *APIHandlers) HandleTaskInput(w http.ResponseWriter, r *http.Request) {
	taskIDStr := r.PathValue("taskID")
	taskID, err := strconv.ParseUint(taskIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid task ID", http.StatusBadRequest)
		return
	}

	taskState := h.manager.processor.GetTask(taskID)

	info, err := os.Stat(taskState.Input)
	if err != nil {
		h.logger.Error("input file stat failed", "task_id", taskID, "path", taskState.Input, "error", err)
		http.Error(w, "input file not found", http.StatusInternalServerError)
		return
	}

	file, err := os.Open(taskState.Input)
	if err != nil {
		h.logger.Error("input file open failed", "task_id", taskID, "error", err)
		http.Error(w, "cannot open input file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	io.Copy(w, file)
}

// HandleTaskProgress handles POST /api/v1/worker/task/progress
func (h *APIHandlers) HandleTaskProgress(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkerID string  `json:"worker_id"`
		TaskID   uint64  `json:"task_id"`
		Progress float64 `json:"progress"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.manager.ReportProgress(req.WorkerID, req.TaskID, req.Progress); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Signal the worker to abort if the task was cancelled
	if h.manager.ShouldCancelTask(req.TaskID) {
		http.Error(w, "task cancelled", http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

// HandleTaskComplete handles POST /api/v1/worker/task/complete
// On success, the request body is the transcoded output binary stream.
// On failure, query parameters carry the error info.
func (h *APIHandlers) HandleTaskComplete(w http.ResponseWriter, r *http.Request) {
	taskIDStr := r.URL.Query().Get("task_id")
	workerID := r.URL.Query().Get("worker_id")
	successStr := r.URL.Query().Get("success")
	errorMsg := r.URL.Query().Get("error_message")

	taskID, err := strconv.ParseUint(taskIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid task ID", http.StatusBadRequest)
		return
	}
	success := successStr == "true"

	if !success {
		if err := h.manager.CompleteTask(workerID, taskID, false, errorMsg); err != nil {
			h.logger.Error("task complete (failure) failed", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
		return
	}

	// Write the streamed output to a temp file on the main node.
	tempPath, writeErr := h.manager.processor.WriteTaskOutput(taskID, r.Body)
	if writeErr != nil {
		h.logger.Error("failed to write task output", "task_id", taskID, "error", writeErr)
		if err := h.manager.CompleteTask(workerID, taskID, false, writeErr.Error()); err != nil {
			h.logger.Error("failed to mark task failed after write error", "error", err)
		}
		http.Error(w, writeErr.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.manager.CompleteTask(workerID, taskID, true, ""); err != nil {
		h.logger.Error("failed to complete task", "task_id", taskID, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.logger.Info("task output received from worker", "task_id", taskID, "temp_file", tempPath)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}
