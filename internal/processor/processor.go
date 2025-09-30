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

	ffmpegReady  bool
	ffmpegBinary func() string

	logger *slog.Logger
	config config.Config
}

const defaultFFmpegPath = "ffmpeg"

// NewProcessor creates a new task processor.
func NewProcessor(config config.Config, logger *slog.Logger) *Processor {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}

	processor := &Processor{
		config: config,
		queue:  make(chan *task, 100),
		tasks:  map[uint64]*task{},
		logger: logger,
	}

	processor.ffmpegBinary = sync.OnceValue(func() string {
		defer func() {
			processor.ffmpegReady = true
		}()
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
	go processor.ffmpegBinary()

	return processor
}

func (p *Processor) FFmpegBinary() string {
	if !p.ffmpegReady {
		return ""
	}
	return p.ffmpegBinary()
}

// StartWorker begins a background worker that processes pending tasks.
func (p *Processor) StartWorker() {
	p.logger.Info("starting task processor worker")

	go func() {
		for task := range p.queue {
			p.processTask(task)
		}
	}()
}

func (p *Processor) HasTask(path, preset string) bool {
	p.tasksMu.RLock()
	defer p.tasksMu.RUnlock()

	for _, t := range p.tasks {
		if t.Input == path && t.Preset == preset && !t.cancelled.Load() {
			return true
		}
	}
	return false
}

// AddTask creates and enqueues a new transcoding task.
func (p *Processor) AddTask(path, preset string) {
	p.tasksMu.Lock()
	defer p.tasksMu.Unlock()

	id := p.taskAI.Add(1)
	task := newTask(id, path, preset)
	p.tasks[task.ID] = task
	p.logger.Info("task added to queue",
		"task_id", task.ID,
		"input", task.Input,
		"preset", task.Preset)
	p.queue <- task
}

// CancelTask attempts to cancel a task by ID.
func (p *Processor) CancelTask(id uint64) error {
	p.logger.Info("cancelling task", "task_id", id)
	p.tasks[id].cancelled.Store(true)
	if p.tasks[id].cmd != nil {
		p.tasks[id].cmd.Process.Signal(syscall.SIGTERM)
	}

	return nil // Task not found
}

// getProfile retrieves a transcoding profile by name.
func (p *Processor) getProfile(name string) transcoding.Profile {
	for _, p := range p.config.Profiles {
		if p.Name == name {
			return p
		}
	}
	p.logger.Warn("profile not found", "profile_name", name)
	return transcoding.Profile{}
}
