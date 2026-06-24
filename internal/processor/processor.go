package processor

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	ffmpeg "github.com/u2takey/ffmpeg-go"

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

	// Callback for when tasks reach waiting_for_resolution status
	onWaitingForResolution func(TaskState)
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

	p.tasksMu.RLock()
	task, ok := p.tasks[id]
	p.tasksMu.RUnlock()
	if !ok {
		return fmt.Errorf("task %d not found", id)
	}

	task.cancelled.Store(true)
	task.MarkCancelled()

	if task.cmd != nil && task.cmd.Process != nil {
		task.cmd.Process.Signal(syscall.SIGTERM)
	}

	return nil
}

// IsCancelled returns true if the task's cancelled flag is set.
func (p *Processor) IsCancelled(taskID uint64) bool {
	p.tasksMu.RLock()
	task, ok := p.tasks[taskID]
	p.tasksMu.RUnlock()
	if !ok {
		return false
	}
	return task.cancelled.Load()
}

// SetOnWaitingForResolutionCallback sets a callback that gets called when a task transitions to waiting_for_resolution
func (p *Processor) SetOnWaitingForResolutionCallback(callback func(TaskState)) {
	p.onWaitingForResolution = callback
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

// AcquiredTask is returned to a remote worker when it acquires a task from the queue.
type AcquiredTask struct {
	ID            uint64            `json:"task_id"`
	Preset        string            `json:"preset"`
	Params        map[string]string `json:"params"`
	FFmpegPath    string            `json:"ffmpeg_path"`
	InputSize     int64             `json:"input_size"`
	TotalDuration float64           `json:"total_duration"`
	OutputExt     string            `json:"output_ext"`
}

// DequeueForWorker atomically takes the next pending task from the channel
// and assigns it to a remote worker. Returns nil if no tasks are available.
func (p *Processor) DequeueForWorker(workerID string) (*AcquiredTask, error) {
	select {
	case task := <-p.queue:
		if task.cancelled.Load() {
			task.MarkCancelled()
			return nil, nil
		}

		task.WorkerID = workerID
		task.MarkProcessing()

		p.logger.Info("task assigned to remote worker",
			"task_id", task.ID, "worker_id", workerID)

		duration, size, preset, err := p.probeAndValidate(task)
		if err != nil {
			task.MarkFailed(err)
			return nil, err
		}

		// Create temp file path for output
		task.TempFile, err = p.tempFile(task.Input)
		if err != nil {
			p.logger.Error("failed to create temp file", "task_id", task.ID, "error", err)
			task.MarkFailed(fmt.Errorf("failed to create temp file: %w", err))
			return nil, err
		}

		// Final check: if the task was cancelled during probe/setup,
		// do not hand it out to a worker.
		if task.cancelled.Load() {
			task.MarkCancelled()
			return nil, nil
		}

		return &AcquiredTask{
			ID:            task.ID,
			Preset:        preset.Name,
			Params:        preset.Params,
			FFmpegPath:    p.ffmpegBinary(),
			InputSize:     size,
			TotalDuration: duration,
			OutputExt:     path.Ext(task.Input),
		}, nil
	default:
		return nil, nil // No tasks available
	}
}

// probeAndValidate probes the input file and validates the preset.
// Returns duration, file size, and the resolved profile.
func (p *Processor) probeAndValidate(task *task) (float64, int64, transcoding.Profile, error) {
	a, err := ffmpeg.Probe(task.Input)
	if err != nil {
		return 0, 0, transcoding.Profile{}, fmt.Errorf("probe failed: %w", err)
	}

	duration, err := probeDuration(a)
	if err != nil {
		return 0, 0, transcoding.Profile{}, fmt.Errorf("duration parse failed: %w", err)
	}

	preset := p.getProfile(task.Preset)
	if preset.Name == "" {
		return 0, 0, transcoding.Profile{}, fmt.Errorf("invalid preset: %s", task.Preset)
	}

	info, err := os.Stat(task.Input)
	if err != nil {
		return 0, 0, transcoding.Profile{}, fmt.Errorf("stat failed: %w", err)
	}

	return duration, info.Size(), preset, nil
}

// UpdateProgress sets the progress for a task (called by remote workers).
func (p *Processor) UpdateProgress(taskID uint64, progress float64) error {
	p.tasksMu.RLock()
	task, ok := p.tasks[taskID]
	p.tasksMu.RUnlock()
	if !ok {
		return fmt.Errorf("task %d not found", taskID)
	}
	task.SetProgress(progress)
	return nil
}

// RequeueTask resets a task to pending and puts it back in the queue.
// Used when a worker disconnects so another worker can pick up the task.
func (p *Processor) RequeueTask(taskID uint64) error {
	p.tasksMu.RLock()
	task, ok := p.tasks[taskID]
	p.tasksMu.RUnlock()
	if !ok {
		return fmt.Errorf("task %d not found", taskID)
	}
	// Do not requeue a cancelled task.
	if task.cancelled.Load() {
		task.MarkCancelled()
		return nil
	}
	task.Status = TaskStatusPending
	task.WorkerID = ""
	task.Progress = 0
	p.queue <- task
	p.logger.Info("task requeued after worker disconnect", "task_id", taskID)
	return nil
}

// CompleteTask transitions a remotely-processed task to its terminal state.
func (p *Processor) CompleteTask(taskID uint64, success bool, errMsg string) error {
	p.tasksMu.RLock()
	task, ok := p.tasks[taskID]
	p.tasksMu.RUnlock()
	if !ok {
		return fmt.Errorf("task %d not found", taskID)
	}

	// Reject completion if the task was cancelled
	if task.cancelled.Load() {
		return fmt.Errorf("task %d was cancelled", taskID)
	}

	if !success {
		task.MarkFailed(fmt.Errorf("%s", errMsg))
		return nil
	}

	task.MarkWaitingForResolution()
	if p.onWaitingForResolution != nil {
		p.onWaitingForResolution(task.State())
	}
	return nil
}

// WriteTaskOutput writes the remote worker's output stream to the task's temp file.
func (p *Processor) WriteTaskOutput(taskID uint64, reader io.Reader) (string, error) {
	p.tasksMu.RLock()
	task, ok := p.tasks[taskID]
	p.tasksMu.RUnlock()
	if !ok {
		return "", fmt.Errorf("task %d not found", taskID)
	}

	if task.cancelled.Load() {
		return "", fmt.Errorf("task %d was cancelled", taskID)
	}

	f, err := os.Create(task.TempFile)
	if err != nil {
		return "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, reader); err != nil {
		os.Remove(task.TempFile)
		return "", fmt.Errorf("failed to write output data: %w", err)
	}
	f.Close()

	// Sanity check: validate the output file with ffprobe.
	// A valid transcode should always produce a probe-able file.
	if _, err := transcoding.Probe(task.TempFile); err != nil {
		os.Remove(task.TempFile)
		return "", fmt.Errorf("output validation failed (ffprobe): %w", err)
	}

	return task.TempFile, nil
}
