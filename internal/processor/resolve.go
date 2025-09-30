package processor

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// ResolveTask handles the final resolution of a completed task.
func (p *Processor) ResolveTask(ctx context.Context, taskID uint64, replace bool) {
	log := p.logger.With("task_id", taskID, "replace", replace)

	log.Info("resolving task")

	task := p.tasks[taskID]

	if task.Status != TaskStatusWaitingForResolution {
		log.Error("task is not in a resolvable state", "status", task.Status)
		return
	}

	task.MarkStatusReplacing()

	go func() {
		// Perform the actual resolution
		err := p.resolveTask(task, replace)

		if err != nil {
			log.Error("task resolution failed", "error", err)
			task.MarkFailed(err)
		} else {
			p.logger.Info("task resolved successfully")
			task.MarkCompleted()
		}
	}()

}

// resolveTask completes a task that's waiting for resolution by either
// keeping the original file or replacing it with the transcoded version.
func (p *Processor) resolveTask(task *task, replace bool) error {
	log := p.logger.With("task_id", task.ID, "replace", replace, "temp_file", task.TempFile, "input_file", task.Input)

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

	err := p.replaceFile(task.TempFile, task.Input)
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
// Uses a safer approach with a single temporary file in the same directory as the destination.
func (p *Processor) replaceFile(src, dst string) error {
	log := p.logger.With("src", src, "dst", dst)

	log.Debug("replacing file")

	// Create a temporary file in the same directory as the destination
	tmpFile := filepath.Join(filepath.Dir(dst), ".tmp_"+filepath.Base(dst))

	// Open the source file for reading
	srcFile, err := os.Open(src)
	if err != nil {
		log.Error("failed to open source file", "error", err)
		return err
	}
	defer srcFile.Close()

	// Get source file size for preallocation
	srcInfo, err := srcFile.Stat()
	if err != nil {
		log.Error("failed to get source file stats", "error", err)
		return err
	}
	srcSize := srcInfo.Size()

	// Create the temporary file for writing
	tmpDstFile, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		log.Error("failed to create temporary file", "error", err)
		return err
	}
	defer func() {
		if tmpDstFile != nil {
			tmpDstFile.Close()
		}
	}()

	// Preallocate space for the file to prevent fragmentation and ensure space is available
	if srcSize > 0 {
		fd := int(tmpDstFile.Fd())
		err = unix.Fallocate(fd, 0, 0, srcSize)
		if err != nil {
			log.Warn("file preallocation failed, continuing with regular copy", "error", err)
			// Continue despite preallocation error - it's just an optimization
		} else {
			log.Debug("preallocated file space", "size", srcSize)
		}
	}

	// Copy the contents from the source file to the temporary file
	bytesWritten, err := io.Copy(tmpDstFile, srcFile)
	if err != nil {
		log.Error("failed to copy file contents", "error", err)
		tmpDstFile.Close()
		tmpDstFile = nil
		os.Remove(tmpFile) // Clean up temp file on error
		return err
	}

	// Close the temporary file before renaming
	if err = tmpDstFile.Close(); err != nil {
		log.Error("failed to close temporary file", "error", err)
		tmpDstFile = nil
		os.Remove(tmpFile)
		return err
	}
	tmpDstFile = nil

	// Preserve original file permissions if destination file exists
	if fileInfo, err := os.Stat(dst); err == nil {
		if err = os.Chmod(tmpFile, fileInfo.Mode()); err != nil {
			log.Warn("failed to preserve file permissions", "error", err)
			// Continue despite permission error
		}
	}

	// Atomically rename the temporary file to the destination
	if err = os.Rename(tmpFile, dst); err != nil {
		log.Error("failed to rename temporary file to destination", "error", err)
		os.Remove(tmpFile) // Clean up temp file on error
		return err
	}

	p.logger.Debug("file copied successfully", "bytes", bytesWritten)
	p.logger.Info("file replaced successfully", "dst", dst)
	return nil
}
