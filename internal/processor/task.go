// Package processor provides transcoding task management and execution capabilities.
package processor

import (
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

// TaskStatus represents the current state of a transcoding task.
type TaskStatus string

// Task status constants define the possible states of a transcoding task.
const (
	// TaskStatusPending indicates the task is waiting to be processed.
	TaskStatusPending TaskStatus = "pending"

	// TaskStatusProcessing indicates the task is currently being processed.
	TaskStatusProcessing TaskStatus = "processing"

	// TaskStatusWaitingForResolution indicates the task has completed processing but
	// requires user action to determine what to do with the output file.
	TaskStatusWaitingForResolution TaskStatus = "waiting_for_resolution"

	// TaskStatsuReplacing indicates the task is in the process of replacing the original file.
	TaskStatusReplacing TaskStatus = "replacing"

	// TaskStatusCompleted indicates the task has successfully completed.
	TaskStatusCompleted TaskStatus = "completed"

	// TaskStatusCancelled indicates the task was manually cancelled by the user.
	TaskStatusCancelled TaskStatus = "cancelled"

	// TaskStatusFailed indicates the task encountered an error and could not complete.
	TaskStatusFailed TaskStatus = "failed"
)

// task represents a transcoding job with metadata about the process.
type task struct {
	// Core identification
	ID       uint64    // Unique task identifier
	CreateAt time.Time // When the task was created

	// Input/Output configuration
	Input    string // Source file path
	Preset   string // Transcoding profile name
	TempFile string // Temporary output file path

	// Status information
	Status   TaskStatus // Current state of the task
	Progress float64    // Processing progress (0.0 to 1.0)
	Error    error      // Error information if task failed

	// Runtime data
	cancelled atomic.Bool // Indicates if the task was cancelled
	cmd       *exec.Cmd   // FFmpeg command reference
	startedAt time.Time   // When processing started
	endedAt   time.Time   // When processing completed
}

// newTask creates a new transcoding task in pending state.
func newTask(id uint64, inputPath, presetName string) *task {
	return &task{
		ID:       id,
		Input:    inputPath,
		Preset:   presetName,
		Status:   TaskStatusPending,
		Progress: 0,
		CreateAt: time.Now(),
	}
}

// MarkProcessing transitions the task to processing state.
func (t *task) MarkProcessing() {
	t.Status = TaskStatusProcessing
	t.startedAt = time.Now()
}

// MarkWaitingForResolution transitions the task to waiting for resolution state.
func (t *task) MarkWaitingForResolution() {
	t.Status = TaskStatusWaitingForResolution
	t.endedAt = time.Now()
}

// MarkWaitingForResolution transitions the task to waiting for resolution state.
func (t *task) MarkStatusReplacing() {
	t.Status = TaskStatusReplacing
	t.endedAt = time.Now()
}

// MarkCompleted transitions the task to completed state.
func (t *task) MarkCompleted() {
	t.Status = TaskStatusCompleted
	t.Progress = 1.0
	t.endedAt = time.Now()
}

// MarkFailed transitions the task to failed state with an error.
func (t *task) MarkFailed(err error) {
	t.Status = TaskStatusFailed
	t.Error = err
	t.endedAt = time.Now()
}

// MarkCancelled transitions the task to cancelled state.
func (t *task) MarkCancelled() {
	t.Status = TaskStatusCancelled
	t.endedAt = time.Now()
}

// IsActive returns true if the task is currently processing.
func (t *task) IsActive() bool {
	return t.Status == TaskStatusProcessing
}

// IsPending returns true if the task is waiting to start.
func (t *task) IsPending() bool {
	return t.Status == TaskStatusPending
}

// IsFinished returns true if the task has reached a terminal state.
func (t *task) IsFinished() bool {
	return t.Status == TaskStatusCompleted ||
		t.Status == TaskStatusFailed ||
		t.Status == TaskStatusCancelled
}

// SetProgress updates the progress percentage (0.0 to 1.0).
func (t *task) SetProgress(progress float64) {
	// Constrain progress between 0 and 1
	if progress < 0 {
		progress = 0
	} else if progress > 1 {
		progress = 1
	}
	t.Progress = progress
}

// SetCommand assigns a command to the task.
func (t *task) SetCommand(cmd *exec.Cmd) {
	t.cmd = cmd
}

func (t *task) Cancel() error {
	t.cmd.Process.Signal(syscall.SIGTERM)
	t.cancelled.Store(true)
	return nil
}

func (t *task) State() TaskState {
	return TaskState{
		ID:       t.ID,
		CreateAt: t.CreateAt,
		Input:    t.Input,
		Preset:   t.Preset,
		TempFile: t.TempFile,
		Status:   t.Status,
		Progress: t.Progress,
		Error:    t.Error,
	}
}
