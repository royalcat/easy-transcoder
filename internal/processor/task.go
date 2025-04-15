package processor

import "os/exec"

type TaskStatus string

const (
	TaskStatusPending              TaskStatus = "pending"
	TaskStatusProcessing           TaskStatus = "processing"
	TaskStatusWaitingForResolution TaskStatus = "waiting_for_resolution"
	TaskStatusCompleted            TaskStatus = "completed"
	TaskStatusCancelled            TaskStatus = "cancelled"
	TaskStatusFailed               TaskStatus = "failed"
)

type Task struct {
	ID     uint64
	Input  string
	Preset string

	Status   TaskStatus
	Progress float64
	TempFile string
	Error    error

	cmd *exec.Cmd
}
