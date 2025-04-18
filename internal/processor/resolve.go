package processor

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

// ResolveTask handles the final resolution of a completed task.
func (q *Processor) ResolveTask(ctx context.Context, taskID uint64, replace bool) {
	log := q.logger.With("task_id", taskID, "replace", replace)

	log.Info("resolving task")

	task := q.tasks[taskID]

	if task.Status != TaskStatusWaitingForResolution {
		log.Error("task is not in a resolvable state", "status", task.Status)
		return
	}

	task.MarkStatusReplacing()

	// Perform the actual resolution
	err := q.resolveTask(task, replace)

	if err != nil {
		log.Error("task resolution failed", "error", err)
		task.MarkFailed(err)
	} else {
		q.logger.Info("task resolved successfully")
		task.MarkCompleted()
	}
}

// resolveTask completes a task that's waiting for resolution by either
// keeping the original file or replacing it with the transcoded version.
func (q *Processor) resolveTask(task *task, replace bool) error {
	log := q.logger.With("task_id", task.ID, "replace", replace, "temp_file", task.TempFile, "input_file", task.Input)

	if !replace {
		// If not replacing, just clean up temp file
		log.Info("keeping original file")

		if task.TempFile == "" {
			log.Warn("no temp file to clean up")
			return nil
		}

		err := os.RemoveAll(filepath.Dir(task.TempFile))
		if err != nil {
			log.Error("failed to remove temp file", "error", err)
			return err
		}

		return nil
	}

	// Replace the original file with the transcoded version
	log.Info("replacing original file with transcoded version")

	err := q.replaceFile(task.TempFile, task.Input)
	if err != nil {
		log.Error("file replacement failed", "error", err)
		return err
	}

	// Clean up temp directory
	if task.TempFile != "" {
		log.Debug("cleaning up temp directory", "dir", filepath.Dir(task.TempFile))

		err = os.RemoveAll(filepath.Dir(task.TempFile))
		if err != nil {
			log.Error("failed to remove temp directory",
				"task_id", task.ID,
				"dir", filepath.Dir(task.TempFile),
				"error", err)
			// Not returning error here as the main operation succeeded
		}
	}

	return nil
}

// replaceFile replaces the destination file with the contents of the source file.
func (q *Processor) replaceFile(src, dst string) error {
	log := q.logger.With("src", src, "dst", dst)

	log.Debug("replacing file")

	// Open the source file for reading
	srcFile, err := os.Open(src)
	if err != nil {
		log.Error("failed to open source file", "error", err)
		return err
	}
	defer srcFile.Close()

	// Create the destination file for writing
	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		log.Error("failed to create destination file", "error", err)
		return err
	}
	defer dstFile.Close()

	// Copy the contents from the source file to the destination file
	bytesWritten, err := io.Copy(dstFile, srcFile)
	if err != nil {
		log.Error("failed to copy file contents", "error", err)
		return err
	}

	q.logger.Debug("file copied successfully", "bytes", bytesWritten)

	q.logger.Info("file replaced successfully", "dst", dst)
	return nil
}
