package processor

import (
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/royalcat/easy-transcoder/internal/config"
	"github.com/royalcat/easy-transcoder/internal/transcoding"
)

// Processor manages a queue of transcoding tasks.
type Processor struct {
	taskAI  atomic.Uint64
	queue   chan *task
	tasksMu sync.RWMutex
	tasks   map[uint64]*task

	ffmpegPath func() string

	logger *slog.Logger
	config config.Config
}

const defaultFFmpegPath = "ffmpeg"

// NewProcessor creates a new task processor.
func NewProcessor(config config.Config, logger *slog.Logger) *Processor {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}

	ffmpegPath := sync.OnceValue(func() string {
		log := logger.With("component", "setup-custom-ffmpeg", "url", config.CustomFFmpegURL)
		if config.CustomFFmpegURL == "" {
			return defaultFFmpegPath
		} else if strings.HasPrefix(config.CustomFFmpegURL, "https://") || strings.HasPrefix(config.CustomFFmpegURL, "http://") {
			log.Info("custom ffmpeg url detected, downloading and extracting")
			binaryPath := "./custom-ffmpeg"
			err := downloadAndExtract(config.CustomFFmpegURL, binaryPath)
			if err != nil {
				log.Error("failed to download and extract custom ffmpeg", "error", err.Error())
				return defaultFFmpegPath
			}
			log.Info("custom ffmpeg downloaded and extracted")
			return binaryPath
		} else {
			log.Error("invalid ffmpeg url, using default", "url", config.CustomFFmpegURL)
			return defaultFFmpegPath
		}
	})
	go ffmpegPath()

	return &Processor{
		config:     config,
		queue:      make(chan *task, 100),
		tasks:      map[uint64]*task{},
		ffmpegPath: ffmpegPath,
		logger:     logger,
	}
}

// StartWorker begins a background worker that processes pending tasks.
func (q *Processor) StartWorker() {
	q.logger.Info("starting task processor worker")

	go func() {
		for task := range q.queue {
			q.processTask(task)
		}
	}()
}

func (q *Processor) HasTask(path, preset string) bool {
	q.tasksMu.RLock()
	defer q.tasksMu.RUnlock()

	for _, t := range q.tasks {
		if t.Input == path && t.Preset == preset && !t.cancelled.Load() {
			return true
		}
	}
	return false
}

// AddTask creates and enqueues a new transcoding task.
func (q *Processor) AddTask(path, preset string) {
	q.tasksMu.Lock()
	defer q.tasksMu.Unlock()

	id := q.taskAI.Add(1)
	task := newTask(id, path, preset)
	q.tasks[task.ID] = task
	q.logger.Info("task added to queue",
		"task_id", task.ID,
		"input", task.Input,
		"preset", task.Preset)
	q.queue <- task
}

// CancelTask attempts to cancel a task by ID.
func (q *Processor) CancelTask(id uint64) error {
	q.logger.Info("cancelling task", "task_id", id)
	q.tasks[id].cancelled.Store(true)
	if q.tasks[id].cmd != nil {
		q.tasks[id].cmd.Process.Signal(syscall.SIGTERM)
	}

	return nil // Task not found
}

// getProfile retrieves a transcoding profile by name.
func (q *Processor) getProfile(name string) transcoding.Profile {
	for _, p := range q.config.Profiles {
		if p.Name == name {
			return p
		}
	}
	q.logger.Warn("profile not found", "profile_name", name)
	return transcoding.Profile{}
}
