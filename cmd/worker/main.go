// Command easy-transcoder-worker is a remote transcoding worker that connects
// to an easy-transcoder main node, acquires transcoding tasks, processes them
// using FFmpeg (input streamed via stdin pipe, output written to temp file),
// and streams results back.
//
// Usage:
//
//	easy-transcoder-worker --server-url http://host:8080 --api-token <token>
//
// Or via environment variables:
//
//	EASY_TRANSCODER_SERVER_URL=http://host:8080
//	EASY_TRANSCODER_WORKER_API_TOKEN=<token>
//	EASY_TRANSCODER_WORKER_FFMPEG=/path/to/ffmpeg  # optional
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

var (
	serverURL  = flag.String("server-url", "", "URL of the easy-transcoder main node (e.g. http://host:8080)")
	apiToken   = flag.String("api-token", "", "Shared API token for worker authentication")
	ffmpegPath = flag.String("ffmpeg-path", "ffmpeg", "Path to the FFmpeg binary")
)

var httpClient = &http.Client{}

func main() {
	flag.Parse()

	// Resolve from environment if not provided via flags
	if *serverURL == "" {
		*serverURL = os.Getenv("EASY_TRANSCODER_SERVER_URL")
	}
	if *apiToken == "" {
		*apiToken = os.Getenv("EASY_TRANSCODER_WORKER_API_TOKEN")
	}
	if envFFmpeg := os.Getenv("EASY_TRANSCODER_WORKER_FFMPEG"); envFFmpeg != "" {
		*ffmpegPath = envFFmpeg
	}

	if *serverURL == "" || *apiToken == "" {
		fmt.Fprintln(os.Stderr, "Error: --server-url and --api-token are required")
		fmt.Fprintln(os.Stderr, "  Set via flags, or environment variables EASY_TRANSCODER_SERVER_URL and EASY_TRANSCODER_WORKER_API_TOKEN")
		flag.Usage()
		os.Exit(1)
	}

	*serverURL = strings.TrimRight(*serverURL, "/")

	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[worker] ")

	log.Printf("starting worker, server=%s", *serverURL)

	// Register with the main node (gathers system info automatically)
	workerID, heartbeatInterval := register()
	log.Printf("registered as worker %s (heartbeat every %ds)", workerID, heartbeatInterval)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start heartbeat goroutine
	stopHeartbeat := make(chan struct{})
	go heartbeatLoop(workerID, heartbeatInterval, stopHeartbeat)

	// Task acquisition + processing loop
	go taskLoop(workerID)

	<-sigCh
	log.Println("shutting down...")
	close(stopHeartbeat)

	// Send a final heartbeat with no task to signal idle/disconnect
	sendHeartbeat(workerID, nil)
	log.Println("worker stopped")
}

// register sends a registration request to the main node.
func register() (workerID string, heartbeatInterval int) {
	hostname, _ := os.Hostname()
	cpuModel := getCPUModel()
	cpuCores := runtime.NumCPU()
	totalMem := getTotalMemory()
	ffmpegVer := getFFmpegVersion(*ffmpegPath)

	body := map[string]any{
		"hostname":       hostname,
		"cpu_model":      cpuModel,
		"cpu_cores":      cpuCores,
		"total_memory":   totalMem,
		"ffmpeg_version": ffmpegVer,
	}

	resp, err := doJSON("POST", "/api/v1/worker/register", body)
	if err != nil {
		log.Fatalf("registration failed: %v", err)
	}

	var regResp struct {
		WorkerID          string `json:"worker_id"`
		HeartbeatInterval int    `json:"heartbeat_interval"`
	}
	if err := json.Unmarshal(resp, &regResp); err != nil {
		log.Fatalf("registration response parse failed: %v", err)
	}

	return regResp.WorkerID, regResp.HeartbeatInterval
}

// heartbeatLoop sends periodic heartbeats to the main node.
func heartbeatLoop(workerID string, intervalSec int, stop <-chan struct{}) {
	interval := time.Duration(intervalSec) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sendHeartbeat(workerID, currentTaskID)
		case <-stop:
			return
		}
	}
}

// currentTaskID is set when a task is acquired and cleared on completion.
var currentTaskID *uint64

func sendHeartbeat(workerID string, taskID *uint64) {
	body := map[string]any{
		"worker_id":       workerID,
		"current_task_id": taskID,
	}
	_, err := doJSON("POST", "/api/v1/worker/heartbeat", body)
	if err != nil {
		log.Printf("heartbeat failed: %v", err)
	}
}

// taskLoop continuously polls for and processes tasks.
func taskLoop(workerID string) {
	for {
		task := acquireTask(workerID)
		if task == nil {
			time.Sleep(3 * time.Second)
			continue
		}

		log.Printf("acquired task %d (preset=%s, input_size=%d, duration=%.1fs)",
			task.ID, task.Preset, task.InputSize, task.TotalDuration)

		processTask(workerID, task)
	}
}

// acquireTaskResponse is the JSON returned by the task/acquire endpoint.
type acquireTaskResponse struct {
	ID            uint64            `json:"task_id"`
	Preset        string            `json:"preset"`
	Params        map[string]string `json:"params"`
	FFmpegPath    string            `json:"ffmpeg_path"`
	InputSize     int64             `json:"input_size"`
	TotalDuration float64           `json:"total_duration"`
	OutputExt     string            `json:"output_ext"`
}

func acquireTask(workerID string) *acquireTaskResponse {
	body := map[string]string{"worker_id": workerID}
	resp, err := doJSON("POST", "/api/v1/worker/task/acquire", body)
	if err != nil {
		log.Printf("task acquire failed: %v", err)
		return nil
	}

	if resp == nil {
		return nil
	}

	var task acquireTaskResponse
	if err := json.Unmarshal(resp, &task); err != nil {
		log.Printf("task parse failed: %v", err)
		return nil
	}
	return &task
}

// processTask processes a single transcoding task.
// Input is streamed from the main node directly into FFmpeg's stdin.
// Output is written to a temp file (MP4 needs seekable output), then uploaded.
func processTask(workerID string, task *acquireTaskResponse) {
	taskID := task.ID
	currentTaskID = &taskID
	defer func() { currentTaskID = nil }()

	// Create temp directory for output
	tempDir, err := os.MkdirTemp("", "easy-transcoder-worker-*")
	if err != nil {
		log.Printf("failed to create temp dir for task %d: %v", task.ID, err)
		reportCompletion(workerID, task.ID, false, err.Error())
		return
	}
	defer os.RemoveAll(tempDir)

	// Determine output path with proper extension
	ext := task.OutputExt
	if ext == "" {
		ext = ".mp4"
	}
	outputPath := tempDir + "/output" + ext

	// Start HTTP request to stream input from main node
	log.Printf("streaming input for task %d (%d bytes)", task.ID, task.InputSize)
	inputReq, err := http.NewRequest("GET",
		*serverURL+"/api/v1/worker/task/input/"+strconv.FormatUint(task.ID, 10), nil)
	if err != nil {
		log.Printf("input request failed for task %d: %v", task.ID, err)
		reportCompletion(workerID, task.ID, false, err.Error())
		return
	}
	inputReq.Header.Set("Authorization", "Bearer "+*apiToken)

	inputResp, err := httpClient.Do(inputReq)
	if err != nil {
		log.Printf("input request failed for task %d: %v", task.ID, err)
		reportCompletion(workerID, task.ID, false, err.Error())
		return
	}
	defer inputResp.Body.Close()

	if inputResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(inputResp.Body)
		log.Printf("input request returned %d for task %d: %s", inputResp.StatusCode, task.ID, string(body))
		reportCompletion(workerID, task.ID, false, fmt.Sprintf("server returned %d", inputResp.StatusCode))
		return
	}

	// Build FFmpeg command — input via pipe:0, output to temp file
	ffBin := task.FFmpegPath
	if ffBin == "" {
		ffBin = *ffmpegPath
	}

	args := buildFFmpegArgs(ffBin, outputPath, task.Params)
	log.Printf("running ffmpeg: %s", strings.Join(args, " "))

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = inputResp.Body

	// Progress on stdout (clean, no FFmpeg errors mixed in)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("stdout pipe failed for task %d: %v", task.ID, err)
		reportCompletion(workerID, task.ID, false, err.Error())
		return
	}

	// Errors on stderr (captured separately for clean error logging)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("stderr pipe failed for task %d: %v", task.ID, err)
		reportCompletion(workerID, task.ID, false, err.Error())
		return
	}

	var stderrBuf bytes.Buffer

	if err := cmd.Start(); err != nil {
		log.Printf("ffmpeg start failed for task %d: %v", task.ID, err)
		reportCompletion(workerID, task.ID, false, err.Error())
		return
	}

	// Drain stderr into buffer in background
	go func() { io.Copy(&stderrBuf, stderr) }()

	// Parse progress from stdout; progressDone signals cancellation
	progressDone := make(chan struct{})
	go parseProgressLines(workerID, task.ID, task.TotalDuration, stdout, progressDone)

	// Wait for FFmpeg in a goroutine so we can also watch for cancellation
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	var waitErr error
	select {
	case waitErr = <-waitDone:
		// FFmpeg completed normally — progressDone was not closed.
	case <-progressDone:
		// Server signalled cancellation via 409 on progress report.
		// Kill FFmpeg and stop — the server already marked the task
		// as cancelled, so no completion report is needed.
		log.Printf("task %d cancelled by server", task.ID)
		cmd.Process.Kill()
		<-waitDone
		return
	}

	if waitErr != nil {
		log.Printf("ffmpeg failed for task %d: %v\nffmpeg stderr:\n%s", task.ID, waitErr, stderrBuf.String())
		reportCompletion(workerID, task.ID, false, fmt.Sprintf("ffmpeg error: %v\n%s", waitErr, stderrBuf.String()))
		return
	}

	// Read output file
	outputData, err := os.ReadFile(outputPath)
	if err != nil {
		log.Printf("reading output file failed for task %d: %v", task.ID, err)
		reportCompletion(workerID, task.ID, false, err.Error())
		return
	}

	log.Printf("transcoding complete for task %d, output=%d bytes", task.ID, len(outputData))

	// Upload the transcoded output (this also completes the task on the server)
	if err := uploadOutput(workerID, task.ID, outputData); err != nil {
		log.Printf("output upload failed for task %d: %v", task.ID, err)
		reportCompletion(workerID, task.ID, false, err.Error())
		return
	}

	log.Printf("task %d completed successfully", task.ID)
}

// buildFFmpegArgs constructs an FFmpeg command line.
// Input is read from stdin (pipe:0), output goes to the given file path.
func buildFFmpegArgs(ffBin, output string, params map[string]string) []string {
	args := []string{ffBin, "-progress", "pipe:1", "-i", "pipe:0", "-map", "0"}
	for k, v := range params {
		args = append(args, "-"+k, v)
	}
	args = append(args, "-y", output)
	return args
}

// parseProgressLines reads FFmpeg stderr line by line, extracts out_time_ms,
// and reports progress to the main node. Exits when the reader is closed.
func parseProgressLines(workerID string, taskID uint64, totalDuration float64, reader io.Reader, done chan<- struct{}) {
	re := regexp.MustCompile(`out_time_ms=(\d+)`)
	scanner := bufio.NewScanner(reader)
	lastProgress := 0.0

	for scanner.Scan() {
		matches := re.FindStringSubmatch(scanner.Text())
		if len(matches) >= 2 {
			outTimeUs, _ := strconv.ParseInt(matches[1], 10, 64)
			progress := float64(outTimeUs) / totalDuration / 1e6
			if progress > 1.0 {
				progress = 1.0
			}
			if progress < 0 {
				progress = 0
			}

			if progress-lastProgress >= 0.01 || progress >= 1.0 {
				lastProgress = progress
				if !reportProgress(workerID, taskID, progress) {
					close(done) // signal cancellation to processTask
					return
				}
			}
		}
	}
	// Scanner ended normally (FFmpeg exited) — done is intentionally NOT closed
	// so the select in processTask does not race with waitDone.
}

// reportProgress sends a progress update. Returns false if the server signals cancellation (HTTP 409).
func reportProgress(workerID string, taskID uint64, progress float64) bool {
	jsonData, _ := json.Marshal(map[string]any{
		"worker_id": workerID,
		"task_id":   taskID,
		"progress":  progress,
	})

	req, _ := http.NewRequest("POST", *serverURL+"/api/v1/worker/task/progress", bytes.NewReader(jsonData))
	req.Header.Set("Authorization", "Bearer "+*apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("progress report failed for task %d: %v", taskID, err)
		return true // network error, keep going
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return false // server says cancel
	}
	return true
}

// uploadOutput streams the transcoded output bytes to the main node.
func uploadOutput(workerID string, taskID uint64, data []byte) error {
	u, _ := url.Parse(*serverURL + "/api/v1/worker/task/complete")
	q := u.Query()
	q.Set("task_id", strconv.FormatUint(taskID, 10))
	q.Set("worker_id", workerID)
	q.Set("success", "true")
	u.RawQuery = q.Encode()

	req, err := http.NewRequest("POST", u.String(), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+*apiToken)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// reportCompletion sends a task completion (or failure) to the main node.
func reportCompletion(workerID string, taskID uint64, success bool, errMsg string) {
	u, _ := url.Parse(*serverURL + "/api/v1/worker/task/complete")
	q := u.Query()
	q.Set("task_id", strconv.FormatUint(taskID, 10))
	q.Set("worker_id", workerID)
	q.Set("success", strconv.FormatBool(success))
	if errMsg != "" {
		q.Set("error_message", errMsg)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest("POST", u.String(), nil)
	if err != nil {
		log.Printf("completion report failed for task %d: %v", taskID, err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+*apiToken)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("completion report failed for task %d: %v", taskID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("completion report returned %d for task %d: %s", resp.StatusCode, taskID, string(body))
	}
}

// doJSON sends a JSON-encoded request and returns the response body.
// Returns nil body for 204 No Content.
func doJSON(method, path string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequest(method, *serverURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+*apiToken)
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// getCPUModel returns a human-readable CPU model string.
func getCPUModel() string {
	info, err := cpu.Info()
	if err != nil || len(info) == 0 {
		return "unknown"
	}
	return info[0].ModelName
}

// getTotalMemory returns the total system memory in bytes.
func getTotalMemory() uint64 {
	v, err := mem.VirtualMemory()
	if err != nil {
		return 0
	}
	return v.Total
}

// getFFmpegVersion returns the FFmpeg version string, or empty string on failure.
func getFFmpegVersion(path string) string {
	cmd := exec.Command(path, "-version")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	firstLine := strings.Split(string(output), "\n")[0]
	re := regexp.MustCompile(`ffmpeg version\s+([\w\d\.-]+)`)
	matches := re.FindStringSubmatch(firstLine)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}
