package processor

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

func (q *Processor) updateTask(task Task) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, t := range q.tasks {
		if t.ID == task.ID {
			q.tasks[i] = task
		}
	}
}

func (q *Processor) processTask(task Task) {
	task.Status = TaskStatusProcessing
	q.updateTask(task)

	a, err := ffmpeg.Probe(task.Input)
	if err != nil {
		task.Status = TaskStatusFailed
		task.Error = err
		q.updateTask(task)
		return
	}
	totalDuration, err := probeDuration(a)
	if err != nil {
		task.Status = TaskStatusFailed
		task.Error = err
		q.updateTask(task)
		return
	}

	preset := q.getProfile(task.Preset)

	task.TempFile, err = q.tempFile(task.Input)
	if err != nil {
		task.Status = TaskStatusFailed
		task.Error = err
		q.updateTask(task)
		return
	}

	progressCallback := func(prg float64) {
		fmt.Printf("Progress: %.2f%%\n", prg*100)
		task.Progress = prg
		q.updateTask(task)
	}

	progressSock := ffmpegProgressSock(totalDuration, progressCallback)
	defer os.Remove(progressSock)

	task.cmd = preset.Compile(task.Input, task.TempFile, progressSock)

	err = task.cmd.Run()

	if err != nil {
		task.Status = TaskStatusFailed
		task.Error = err
		q.updateTask(task)
		return
	}

	task.Status = TaskStatusWaitingForResolution
	q.updateTask(task)
	return
}

func ffmpegProgressSock(totalDuration float64, progressCallback func(float64)) string {
	sockFileName := path.Join(os.TempDir(), fmt.Sprintf("%d_sock", rand.Int()))
	l, err := net.Listen("unix", sockFileName)
	if err != nil {
		panic(err)
	}

	go func() {
		re := regexp.MustCompile(`out_time_ms=(\d+)`)
		fd, err := l.Accept()
		if err != nil {
			log.Fatal("accept error:", err)
		}
		buf := make([]byte, 16)
		data := ""
		for {
			_, err := fd.Read(buf)
			if err != nil {
				return
			}
			data += string(buf)
			a := re.FindAllStringSubmatch(data, -1)
			prog := float64(0)
			if len(a) > 0 && len(a[len(a)-1]) > 0 {
				c, _ := strconv.Atoi(a[len(a)-1][len(a[len(a)-1])-1])
				prog = float64(c) / totalDuration / 1000000
			}
			if strings.Contains(data, "progress=end") {
				prog = 1
			}

			progressCallback(prog)
		}
	}()

	return sockFileName
}

type probeFormat struct {
	Duration string `json:"duration"`
}

type probeData struct {
	Format probeFormat `json:"format"`
}

func probeDuration(a string) (float64, error) {
	pd := probeData{}
	err := json.Unmarshal([]byte(a), &pd)
	if err != nil {
		return 0, err
	}
	f, err := strconv.ParseFloat(pd.Format.Duration, 64)
	if err != nil {
		return 0, err
	}
	return f, nil
}
