package processor

import (
	"io"
	"os"
	"time"
)

// ResolveTask completes a task that's waiting for resolution by either
// keeping the original file or replacing it with the transcoded version.
func (q *Processor) ResolveTask(task Task, replace bool) {
	// Add a delay to ensure file operations are complete
	time.Sleep(5 * time.Second)

	if !replace {
		// If not replacing, just mark as completed and clean up temp file
		task.MarkCompleted()
		q.updateTask(task)
		os.Remove(task.TempFile)
		return
	}

	// Replace the original file with the transcoded version
	err := replaceFile(task.TempFile, task.Input)
	if err != nil {
		task.MarkFailed(err)
		q.updateTask(task)
		return
	}

	// Clean up and mark task as completed
	os.Remove(task.TempFile)
	task.MarkCompleted()
	q.updateTask(task)
}

// replaceFile replaces the destination file with the contents of the source file.
func replaceFile(src, dst string) error {
	// Open the source file for reading
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Create the destination file for writing
	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// Copy the contents from the source file to the destination file
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	// Clean up the source file
	os.Remove(src)
	return nil
}
