package processor

import (
	"os"
	"path"
	"slices"
	"sync"
	"time"

	"github.com/royalcat/easy-transcoder/internal/config"
	"github.com/royalcat/easy-transcoder/internal/profile"
)

type Processor struct {
	config config.Config

	mu     sync.Mutex
	taskAI uint64
	tasks  []Task
}

func NewQueue(config config.Config) *Processor {
	return &Processor{
		config: config,
		tasks:  []Task{},
	}
}

func (q *Processor) StartWorker() {
	go func() {
		for range time.Tick(time.Second) {
			if len(q.tasks) == 0 {
				continue
			}

			var task Task

			q.mu.Lock()
			for i := range q.tasks {
				if q.tasks[i].Status == TaskStatusPending {
					task = q.tasks[i]
					q.tasks[i].Status = TaskStatusProcessing
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

func (q *Processor) AddTask(path, preset string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.taskAI++

	task := Task{
		ID:     q.taskAI,
		Input:  path,
		Preset: preset,

		Status:   TaskStatusPending,
		Progress: 0,
	}

	q.tasks = append(q.tasks, task)
}

func (q *Processor) GetQueue() []Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	return slices.Clone(q.tasks)
}

func (q *Processor) GetTask(id uint64) Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	var task Task
	for _, t := range q.tasks {
		if t.ID == uint64(id) {
			task = t
			break
		}
	}

	return task
}

func (q *Processor) CancelTask(id uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i := range q.tasks {
		err := q.tasks[i].cmd.Process.Signal(os.Interrupt)
		if err != nil {
			q.tasks[i].Error = err
			q.tasks[i].Status = TaskStatusFailed
		}

		q.tasks[i].Status = TaskStatusCancelled
	}
}

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

func (q *Processor) getProfile(name string) profile.Profile {
	for _, p := range q.config.Profiles {
		if p.Name == name {
			return p
		}
	}
	return profile.Profile{}
}
