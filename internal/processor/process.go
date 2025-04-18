package processor

import (
	"fmt"
	"os"
	"path"
	"strings"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

// only this function can modify not atomic task status
func (q *Processor) processTask(task *task) {
	log := q.logger.With("task_id", task.ID, "input", task.Input, "preset", task.Preset)

	if task.cancelled.Load() {
		log.Info("transcoding was cancelled")
		task.MarkCancelled()
		return
	}

	task.MarkProcessing()

	// Probe the input file
	log.Info("probing input file")
	a, err := ffmpeg.Probe(task.Input)
	if err != nil {
		log.Error("probe failed", "error", err)
		task.MarkFailed(fmt.Errorf("failed to probe input file: %s", err))
		return
	}

	totalDuration, err := probeDuration(a)
	if err != nil {
		log.Error("failed to parse duration", "error", err)
		task.MarkFailed(fmt.Errorf("failed to parse duration: %s", err))
		return
	}
	log.Debug("media duration detected", "duration", totalDuration)

	preset := q.getProfile(task.Preset)
	if preset.Name == "" {
		log.Error("invalid preset")
		task.MarkFailed(fmt.Errorf("invalid preset: %s", task.Preset))
		return
	}

	// Create temporary output file
	task.TempFile, err = q.tempFile(task.Input)
	if err != nil {
		log.Error("failed to create temp file", "task_id", task.ID, "error", err)
		task.MarkFailed(fmt.Errorf("failed to create temp file: %s", err))
		return
	}
	log.Info("temp file created", "task_id", task.ID, "temp_file", task.TempFile)

	// Setup progress tracking
	progressCallback := func(prg float64) {
		log.Debug("transcoding progress", "progress", fmt.Sprintf("%.2f%%", prg*100))
		task.SetProgress(prg)
	}

	progressSock := q.ffmpegProgressSock(totalDuration, progressCallback, task.ID)
	defer os.Remove(progressSock)

	// Prepare and run the command
	cmd := preset.Compile(task.Input, task.TempFile, progressSock)
	task.SetCommand(cmd)

	log.Info("starting transcoding", "command", strings.Join(cmd.Args, " "))

	err = cmd.Run()

	// Ignore error if the task was cancelled
	if task.cancelled.Load() {
		log.Info("transcoding was cancelled")
		task.MarkCancelled()
		return
	}

	if err != nil {
		log.Error("transcoding failed", "error", err)
		task.MarkFailed(fmt.Errorf("transcoding failed: %s", err))
		return
	}

	log.Info("transcoding completed, mark waiting for resolution")
	task.MarkWaitingForResolution()
}

// tempFile creates a temporary file path for transcoding output.
func (q *Processor) tempFile(filename string) (string, error) {
	tempDir := q.config.TempDir
	if tempDir == "" {
		tempDir = path.Join(os.TempDir(), "easy-transcoder")
	}
	q.logger.Debug("creating temp directory", "dir", tempDir)

	err := os.MkdirAll(tempDir, os.ModePerm)
	if err != nil {
		q.logger.Error("failed to create temp directory",
			"dir", tempDir,
			"error", err)
		return "", err
	}

	tempDir, err = os.MkdirTemp(tempDir, "")
	if err != nil {
		q.logger.Error("failed to create temp subdirectory",
			"parent_dir", tempDir,
			"error", err)
		return "", err
	}

	tempFilePath := path.Join(tempDir, path.Base(filename))
	q.logger.Debug("created temp file path", "path", tempFilePath)
	return tempFilePath, nil
}
