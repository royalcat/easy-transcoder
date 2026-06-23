package worker

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/royalcat/easy-transcoder/internal/config"
	"github.com/royalcat/easy-transcoder/internal/processor"
)

// Manager orchestrates remote worker lifecycles.
type Manager struct {
	workersMu sync.RWMutex
	workers   map[string]*Worker

	config    config.WorkerConfig
	logger    *slog.Logger
	processor *processor.Processor
}

// NewManager creates a new worker manager.
func NewManager(cfg config.WorkerConfig, proc *processor.Processor, logger *slog.Logger) *Manager {
	return &Manager{
		workers:   make(map[string]*Worker),
		config:    cfg,
		logger:    logger,
		processor: proc,
	}
}

// Enabled returns true when the worker API is configured with an API token.
func (m *Manager) Enabled() bool {
	return m.config.APIToken != ""
}

// Register creates a new worker record and returns its assigned ID.
// If a worker with the same hostname re-registers, the old record is replaced.
func (m *Manager) Register(req RegisterRequest) (*Worker, error) {
	id := generateWorkerID(req.Hostname)

	m.workersMu.Lock()
	defer m.workersMu.Unlock()

	// Replace any existing worker with the same hostname.
	for existingID, w := range m.workers {
		if w.Hostname == req.Hostname {
			delete(m.workers, existingID)
			break
		}
	}

	w := &Worker{
		ID:            id,
		Hostname:      req.Hostname,
		CPUModel:      req.CPUModel,
		CPUCores:      req.CPUCores,
		TotalMemory:   req.TotalMemory,
		FFmpegVersion: req.FFmpegVersion,
		RegisteredAt:  time.Now(),
		lastHeartbeat: time.Now(),
	}
	m.workers[id] = w

	m.logger.Info("worker registered", "worker_id", id, "hostname", req.Hostname)
	return w, nil
}

// Heartbeat updates a worker's liveness timestamp and current task.
func (m *Manager) Heartbeat(workerID string, taskID *uint64) error {
	m.workersMu.RLock()
	w, ok := m.workers[workerID]
	m.workersMu.RUnlock()

	if !ok {
		return fmt.Errorf("worker %s not found", workerID)
	}

	w.UpdateHeartbeat(taskID)
	return nil
}

// AcquireTask attempts to dequeue a pending task and assign it to a worker.
// Returns nil if no pending tasks are available.
func (m *Manager) AcquireTask(workerID string) (*processor.AcquiredTask, error) {
	m.workersMu.RLock()
	_, ok := m.workers[workerID]
	m.workersMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("worker %s not registered", workerID)
	}

	return m.processor.DequeueForWorker(workerID)
}

// ReportProgress updates a task's progress from a remote worker.
func (m *Manager) ReportProgress(workerID string, taskID uint64, progress float64) error {
	m.workersMu.RLock()
	_, ok := m.workers[workerID]
	m.workersMu.RUnlock()
	if !ok {
		return fmt.Errorf("worker %s not registered", workerID)
	}
	return m.processor.UpdateProgress(taskID, progress)
}

// CompleteTask marks a remotely-processed task as completed or failed.
func (m *Manager) CompleteTask(workerID string, taskID uint64, success bool, errMsg string) error {
	m.workersMu.RLock()
	_, ok := m.workers[workerID]
	m.workersMu.RUnlock()
	if !ok {
		return fmt.Errorf("worker %s not registered", workerID)
	}
	return m.processor.CompleteTask(taskID, success, errMsg)
}

// GetWorkers returns a snapshot of all registered workers' public state.
func (m *Manager) GetWorkers() []WorkerState {
	m.workersMu.RLock()
	defer m.workersMu.RUnlock()

	timeout := time.Duration(m.config.HeartbeatTimeout) * time.Second
	states := make([]WorkerState, 0, len(m.workers))
	for _, w := range m.workers {
		states = append(states, w.State(timeout))
	}
	return states
}

// ShouldCancelTask returns true if the given task has been cancelled on the main node.
func (m *Manager) ShouldCancelTask(taskID uint64) bool {
	return m.processor.IsCancelled(taskID)
}

// GetWorkerName returns the hostname for a worker ID, or the ID itself if not found.
func (m *Manager) GetWorkerName(workerID string) string {
	m.workersMu.RLock()
	defer m.workersMu.RUnlock()
	if w, ok := m.workers[workerID]; ok {
		return w.Hostname
	}
	return workerID
}

// StartDisconnectionScanner launches a background goroutine that periodically
// checks for dead workers and fails their assigned tasks.
func (m *Manager) StartDisconnectionScanner() {
	timeout := time.Duration(m.config.HeartbeatTimeout) * time.Second
	scanInterval := timeout
	if scanInterval < 5*time.Second {
		scanInterval = 5 * time.Second
	}

	go func() {
		ticker := time.NewTicker(scanInterval)
		defer ticker.Stop()
		for range ticker.C {
			m.handleDeadWorkers()
		}
	}()
}

func (m *Manager) handleDeadWorkers() {
	timeout := time.Duration(m.config.HeartbeatTimeout) * time.Second
	m.workersMu.Lock()
	defer m.workersMu.Unlock()

	for id, w := range m.workers {
		if !w.IsAlive(timeout) {
			m.logger.Warn("worker appears dead", "worker_id", id, "hostname", w.Hostname)

			taskID := w.CurrentTaskID()
			if taskID != nil {
				if err := m.processor.RequeueTask(*taskID); err != nil {
					m.logger.Error("failed to requeue task for dead worker",
						"worker_id", id, "task_id", *taskID, "error", err)
				}
			}
			delete(m.workers, id)
		}
	}
}

// generateWorkerID creates a unique worker ID from the hostname plus a random suffix.
func generateWorkerID(hostname string) string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%s-%s", hostname, hex.EncodeToString(b))
}
