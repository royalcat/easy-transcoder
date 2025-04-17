package processor

import (
	"os"
	"path"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/royalcat/easy-transcoder/internal/config"
	"github.com/royalcat/easy-transcoder/internal/profile"
)

// Processor manages a queue of transcoding tasks.
type Processor struct {
	config config.Config

	mu     sync.Mutex
	taskAI uint64
	tasks  []Task
}

// NewQueue creates a new task processor.
func NewQueue(config config.Config) *Processor {
	return &Processor{
		config: config,
		tasks:  []Task{},
	}
}

// StartWorker begins a background worker that processes pending tasks.
func (q *Processor) StartWorker() {
	go func() {
		for range time.Tick(time.Second) {
			if len(q.tasks) == 0 {
				continue
			}

			var task Task

			q.mu.Lock()
			for i := range q.tasks {
				if q.tasks[i].IsPending() {
					task = q.tasks[i]
					q.tasks[i].MarkProcessing()
					break
				}
			}
			q.mu.Unlock()

			if task.ID == 0 {
				continue
			}

			q.processTask(task)
		}
	}()
}

// AddTask creates and enqueues a new transcoding task.
func (q *Processor) AddTask(path, preset string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.taskAI++
	task := NewTask(q.taskAI, path, preset)
	q.tasks = append(q.tasks, task)
}

// GetQueue returns a copy of all tasks in the queue.
func (q *Processor) GetQueue() []Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	return slices.Clone(q.tasks)
}

// GetTask retrieves a task by ID.
func (q *Processor) GetTask(id uint64) Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	var task Task
	for _, t := range q.tasks {
		if t.ID == id {
			task = t
			break
		}
	}

	return task
}

// CancelTask attempts to cancel a task by ID.
func (q *Processor) CancelTask(id uint64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i := range q.tasks {
		if q.tasks[i].ID == id {
			// Only cancel if in a cancellable state
			if q.tasks[i].IsActive() {
				if q.tasks[i].cmd != nil && q.tasks[i].cmd.Process != nil {
					err := q.tasks[i].cmd.Process.Signal(syscall.SIGTERM)
					if err != nil {
						q.tasks[i].MarkFailed(err)
						return err
					}
				}
			} else if q.tasks[i].IsPending() {
				// Just mark as cancelled if pending
			} else {
				// Task is already completed or in a state that can't be cancelled
				return nil
			}

			q.tasks[i].MarkCancelled()
			return nil
		}
	}

	return nil // Task not found
}

// tempFile creates a temporary file path for transcoding output.
func (q *Processor) tempFile(filename string) (string, error) {
	tempDir := q.config.TempDir
	if tempDir == "" {
		tempDir = path.Join(os.TempDir(), "easy-transcoder")
	}
	err := os.MkdirAll(tempDir, os.ModePerm)
	if err != nil {
		return "", err
	}

	tempDir, err = os.MkdirTemp(tempDir, "")
	if err != nil {
		return "", err
	}
	tempFilePath := path.Join(tempDir, path.Base(filename))
	return tempFilePath, nil
}

// getProfile retrieves a transcoding profile by name.
func (q *Processor) getProfile(name string) profile.Profile {
	for _, p := range q.config.Profiles {
		if p.Name == name {
			return p
		}
	}
	return profile.Profile{}
}
