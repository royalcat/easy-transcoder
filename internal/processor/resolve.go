package processor

import (
	"io"
	"os"
	"time"
)

func (q *Processor) ResolveTask(task Task, replace bool) {
	time.Sleep(5 * time.Second)

	if !replace {
		task.Status = TaskStatusCompleted
		q.updateTask(task)
		os.Remove(task.TempFile)
		return
	}

	err := replaceFile(task.TempFile, task.Input)
	if err != nil {
		task.Status = TaskStatusFailed
		task.Error = err
		q.updateTask(task)
		return
	}

	os.Remove(task.TempFile)

	task.Status = TaskStatusCompleted
	q.updateTask(task)

}

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

	os.Remove(src)

	return nil
}
