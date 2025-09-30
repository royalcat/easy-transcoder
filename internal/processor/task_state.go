package processor

import (
	"cmp"
	"slices"
	"time"
)

// GetTask retrieves a task by ID.
func (p *Processor) GetTask(id uint64) TaskState {
	return p.tasks[id].State()
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
}
