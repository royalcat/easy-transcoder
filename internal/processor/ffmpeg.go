package processor

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
)

func (p *Processor) ffmpegProgressSock(totalDuration float64, progressCallback func(float64), taskID uint64) string {
	sockFileName := path.Join(os.TempDir(), fmt.Sprintf("%d_sock", rand.Int()))
	p.logger.Debug("creating progress socket", "task_id", taskID, "socket", sockFileName)

	l, err := net.Listen("unix", sockFileName)
	if err != nil {
		p.logger.Error("failed to create progress socket", "task_id", taskID, "error", err)
		panic(err)
	}

	go func() {
		re := regexp.MustCompile(`out_time_ms=(\d+)`)
		fd, err := l.Accept()
		if err != nil {
			p.logger.Error("socket accept error", "task_id", taskID, "error", err)
			return
		}

		buf := make([]byte, 16)
		data := ""
		for {
			_, err := fd.Read(buf)
			if err != nil {
				p.logger.Debug("socket read ended", "task_id", taskID, "error", err)
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
				p.logger.Debug("ffmpeg reported progress=end", "task_id", taskID)
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
