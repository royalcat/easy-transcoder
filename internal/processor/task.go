// Package processor provides transcoding task management and execution capabilities.
package processor

import (
	"fmt"
	"os/exec"
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

	// TaskStatusCompleted indicates the task has successfully completed.
	TaskStatusCompleted TaskStatus = "completed"

	// TaskStatusCancelled indicates the task was manually cancelled by the user.
	TaskStatusCancelled TaskStatus = "cancelled"

	// TaskStatusFailed indicates the task encountered an error and could not complete.
	TaskStatusFailed TaskStatus = "failed"
)

// Task represents a transcoding job with metadata about the process.
type Task struct {
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
	cmd       *exec.Cmd // FFmpeg command reference
	startedAt time.Time // When processing started
	endedAt   time.Time // When processing completed
}

// NewTask creates a new transcoding task in pending state.
func NewTask(id uint64, inputPath, presetName string) Task {
	return Task{
		ID:       id,
		Input:    inputPath,
		Preset:   presetName,
		Status:   TaskStatusPending,
		Progress: 0,
		CreateAt: time.Now(),
	}
}

// MarkProcessing transitions the task to processing state.
func (t *Task) MarkProcessing() {
	t.Status = TaskStatusProcessing
	t.startedAt = time.Now()
}

// MarkWaitingForResolution transitions the task to waiting for resolution state.
func (t *Task) MarkWaitingForResolution() {
	t.Status = TaskStatusWaitingForResolution
	t.endedAt = time.Now()
}

// MarkCompleted transitions the task to completed state.
func (t *Task) MarkCompleted() {
	t.Status = TaskStatusCompleted
	t.Progress = 1.0
	t.endedAt = time.Now()
}

// MarkFailed transitions the task to failed state with an error.
func (t *Task) MarkFailed(err error) {
	t.Status = TaskStatusFailed
	t.Error = err
	t.endedAt = time.Now()
}

// MarkCancelled transitions the task to cancelled state.
func (t *Task) MarkCancelled() {
	t.Status = TaskStatusCancelled
	t.endedAt = time.Now()
}

// IsActive returns true if the task is currently processing.
func (t *Task) IsActive() bool {
	return t.Status == TaskStatusProcessing
}

// IsPending returns true if the task is waiting to start.
func (t *Task) IsPending() bool {
	return t.Status == TaskStatusPending
}

// IsFinished returns true if the task has reached a terminal state.
func (t *Task) IsFinished() bool {
	return t.Status == TaskStatusCompleted ||
		t.Status == TaskStatusFailed ||
		t.Status == TaskStatusCancelled
}

// SetProgress updates the progress percentage (0.0 to 1.0).
func (t *Task) SetProgress(progress float64) {
	// Constrain progress between 0 and 1
	if progress < 0 {
		progress = 0
	} else if progress > 1 {
		progress = 1
	}
	t.Progress = progress
}

// SetCommand assigns a command to the task.
func (t *Task) SetCommand(cmd *exec.Cmd) {
	t.cmd = cmd
}

// Duration returns the total time the task has been processing.
func (t *Task) Duration() time.Duration {
	if t.Status == TaskStatusPending {
		return 0
	}

	if t.IsFinished() {
		return t.endedAt.Sub(t.startedAt)
	}

	return time.Since(t.startedAt)
}

// String returns a human-readable representation of the task.
func (t *Task) String() string {
	return fmt.Sprintf("Task %d: %s (Status: %s, Progress: %.1f%%)",
		t.ID, t.Input, t.Status, t.Progress*100)
}
