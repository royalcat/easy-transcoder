package processor

import (
	"cmp"
	"fmt"
	"slices"
	"time"
)

// GetTask retrieves a task by ID.
func (p *Processor) GetTask(id uint64) TaskState {
	p.tasksMu.RLock()
	defer p.tasksMu.RUnlock()
	task, ok := p.tasks[id]
	if !ok {
		return TaskState{}
	}
	return task.State()
}

// FailTask marks a task as failed with the given error.
// Used by the dead worker scanner to fail tasks assigned to disconnected workers.
func (p *Processor) FailTask(taskID uint64, err error) error {
	p.tasksMu.RLock()
	task, ok := p.tasks[taskID]
	p.tasksMu.RUnlock()
	if !ok {
		return fmt.Errorf("task %d not found", taskID)
	}
	task.MarkFailed(err)
	return nil
}

// GetTask retrieves a task by ID.
func (p *Processor) GetQueue() []TaskState {
	var tasks []TaskState
	for _, t := range p.tasks {
		tasks = append(tasks, t.State())
	}

	slices.SortFunc(tasks, func(a, b TaskState) int {
		return cmp.Compare(a.ID, b.ID)
	})

	return tasks
}

type TaskState struct {
	ID uint64 // Unique task identifier

	CreateAt  time.Time // When the task was created
	StartedAt time.Time // When the task started processing
	EndedAt   time.Time // When the task was completed

	// Input/Output configuration
	Input    string // Source file path
	Preset   string // Transcoding profile name
	TempFile string // Temporary output file path

	// Status information
	Status   TaskStatus // Current state of the task
	Progress float64    // Processing progress (0.0 to 1.0)
	Error    error      // Error information if task failed

	// Worker assignment
	WorkerID   string // ID of the worker processing this task
	WorkerName string // Human-readable worker hostname (populated by caller)
}
