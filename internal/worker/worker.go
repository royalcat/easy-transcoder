// Package worker provides remote worker management for distributed transcoding.
package worker

import (
	"sync"
	"time"
)

// Worker represents a registered remote transcoding worker.
type Worker struct {
	ID            string    `json:"id"`
	Hostname      string    `json:"hostname"`
	CPUModel      string    `json:"cpu_model"`
	CPUCores      int       `json:"cpu_cores"`
	TotalMemory   uint64    `json:"total_memory"`
	FFmpegVersion string    `json:"ffmpeg_version"`
	RegisteredAt  time.Time `json:"registered_at"`

	heartbeatMu    sync.RWMutex
	lastHeartbeat  time.Time
	currentTaskMu  sync.RWMutex
	currentTaskID  *uint64 // nil if idle
}

// UpdateHeartbeat records a heartbeat and optionally updates the current task.
func (w *Worker) UpdateHeartbeat(taskID *uint64) {
	w.heartbeatMu.Lock()
	w.lastHeartbeat = time.Now()
	w.heartbeatMu.Unlock()

	w.currentTaskMu.Lock()
	w.currentTaskID = taskID
	w.currentTaskMu.Unlock()
}

// IsAlive returns true if the worker has sent a heartbeat within the timeout duration.
func (w *Worker) IsAlive(timeout time.Duration) bool {
	w.heartbeatMu.RLock()
	defer w.heartbeatMu.RUnlock()
	return time.Since(w.lastHeartbeat) < timeout
}

// CurrentTaskID returns the task ID this worker is currently processing, or nil if idle.
func (w *Worker) CurrentTaskID() *uint64 {
	w.currentTaskMu.RLock()
	defer w.currentTaskMu.RUnlock()
	if w.currentTaskID == nil {
		return nil
	}
	id := *w.currentTaskID
	return &id
}

// State returns a goroutine-safe snapshot of the worker's current state.
func (w *Worker) State(timeout time.Duration) WorkerState {
	w.heartbeatMu.RLock()
	hb := w.lastHeartbeat
	w.heartbeatMu.RUnlock()

	w.currentTaskMu.RLock()
	var ctid uint64
	hasTask := w.currentTaskID != nil
	if hasTask {
		ctid = *w.currentTaskID
	}
	w.currentTaskMu.RUnlock()

	return WorkerState{
		ID:            w.ID,
		Hostname:      w.Hostname,
		CPUModel:      w.CPUModel,
		CPUCores:      w.CPUCores,
		TotalMemory:   w.TotalMemory,
		FFmpegVersion: w.FFmpegVersion,
		RegisteredAt:  w.RegisteredAt,
		LastHeartbeat: hb,
		CurrentTaskID: ctid,
		HasTask:       hasTask,
		Alive:         time.Since(hb) < timeout,
	}
}

// WorkerState is a goroutine-safe snapshot of worker information.
type WorkerState struct {
	ID            string    `json:"id"`
	Hostname      string    `json:"hostname"`
	CPUModel      string    `json:"cpu_model"`
	CPUCores      int       `json:"cpu_cores"`
	TotalMemory   uint64    `json:"total_memory"`
	FFmpegVersion string    `json:"ffmpeg_version"`
	RegisteredAt  time.Time `json:"registered_at"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	CurrentTaskID uint64    `json:"current_task_id"`
	HasTask       bool      `json:"has_task"`
	Alive         bool      `json:"alive"`
}

// RegisterRequest is the JSON body for worker registration.
type RegisterRequest struct {
	Hostname      string `json:"hostname"`
	CPUModel      string `json:"cpu_model"`
	CPUCores      int    `json:"cpu_cores"`
	TotalMemory   uint64 `json:"total_memory"`
	FFmpegVersion string `json:"ffmpeg_version"`
}

// RegisterResponse is the JSON response for a successful worker registration.
type RegisterResponse struct {
	WorkerID          string `json:"worker_id"`
	HeartbeatInterval int    `json:"heartbeat_interval"`
}

// HeartbeatRequest is the JSON body for worker heartbeat.
type HeartbeatRequest struct {
	WorkerID      string  `json:"worker_id"`
	CurrentTaskID *uint64 `json:"current_task_id"`
}
