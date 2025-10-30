package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/a-h/templ"

	"github.com/royalcat/easy-transcoder/assets"
	"github.com/royalcat/easy-transcoder/internal/config"
	"github.com/royalcat/easy-transcoder/internal/processor"
	"github.com/royalcat/easy-transcoder/internal/transcoding"
	"github.com/royalcat/easy-transcoder/ui/elements"
	"github.com/royalcat/easy-transcoder/ui/pages"
)

func main() {
	// Parse configuration first
	cfg, err := config.ParseConfig("config.yaml")
	if err != nil {
		// Use basic logging since we don't have config yet
		slog.Error("failed to parse config", "error", err)
		os.Exit(1)
	}

	// Initialize logger based on configuration
	logger := setupLogger(cfg)
	slog.SetDefault(logger)

	slog.Info("starting easy-transcoder")

	q := processor.NewProcessor(cfg, logger)
	q.StartWorker()

	s := &server{
		Config:    cfg,
		Processor: q,
		logger:    logger,
	}

	// Set up auto-reject callback
	q.SetOnWaitingForResolutionCallback(s.handleTaskWaitingForResolution)

	// Start periodic auto-reject scanner
	go s.startPeriodicAutoRejectScanner()

	templHandler := func(c templ.Component) http.Handler {
		return templ.Handler(c,
			templ.WithErrorHandler(func(r *http.Request, err error) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					s.logger.Error("template error",
						"path", r.URL.Path,
						"error", err,
					)
					http.Error(w, err.Error(), http.StatusInternalServerError)
				})
			}),
		)
	}

	mux := http.NewServeMux()

	assetsRoutes(mux)

	mux.Handle("GET /", http.HandlerFunc(s.pageRoot))
	mux.Handle("GET /resolver", http.HandlerFunc(s.pageResolver))
	mux.Handle("GET /create-task", templHandler(pages.TaskCreation(q.FFmpegBinary(), cfg.Profiles, s.queue())))

	mux.Handle("GET /elements/filepicker", http.HandlerFunc(s.getfilebrowser))
	mux.Handle("GET /elements/fileinfo", http.HandlerFunc(s.getfileinfo))
	mux.Handle("GET /elements/queue", http.HandlerFunc(s.getqueue))
	mux.Handle("GET /elements/status", http.HandlerFunc(s.getstatus))

	// Replaced the single VMAF endpoint with three separate metric endpoints
	mux.Handle("GET /metrics/vmaf", http.HandlerFunc(s.getVMAF))
	mux.Handle("GET /metrics/psnr", http.HandlerFunc(s.getPSNR))
	mux.Handle("GET /metrics/ssim", http.HandlerFunc(s.getSSIM))

	mux.Handle("POST /submit/task", http.HandlerFunc(s.submitTask))
	mux.Handle("POST /submit/task-batch", http.HandlerFunc(s.submitTaskBatch))
	mux.Handle("POST /submit/resolve", http.HandlerFunc(s.submitTaskResolution))
	mux.Handle("POST /submit/cancel", http.HandlerFunc(s.submitTaskCancellation))

	mux.Handle("POST /settings/auto-reject-larger", http.HandlerFunc(s.submitAutoRejectSetting))

	// mux.Handle("GET /elements/profileselector", http.HandlerFunc(s.getprofile))

	// handler := middleware.WithCSP(middleware.CSPConfig{
	// 	ScriptSrc: []string{"cdn.jsdelivr.net", "unpkg.com", "cdnjs.cloudflare.com"},
	// })(mux)

	address := ":8080"
	slog.Info("server starting", "address", address)

	srv := &http.Server{
		Addr:         address,
		Handler:      loggingMiddleware(mux, logger),
		WriteTimeout: 0,
	}

	err = srv.ListenAndServe()
	if err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

// setupLogger creates a logger based on the provided configuration
func setupLogger(cfg config.Config) *slog.Logger {
	// Configure the log level
	level := cfg.GetLogLevel()

	// Configure the handler based on format
	var handler slog.Handler
	if cfg.Logging.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
		})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
		})
	}

	return slog.New(handler)
}

// Logging middleware to log all HTTP requests
func loggingMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response wrapper to capture status code
		rw := &responseWriter{w, http.StatusOK}

		// Process request
		next.ServeHTTP(rw, r)

		// Log request details after processing
		logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.statusCode,
			"duration", time.Since(start),
			"remote_addr", r.RemoteAddr,
		)
	})
}

// responseWriter is a wrapper for http.ResponseWriter that captures the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code before writing it
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

type server struct {
	Config    config.Config
	Processor *processor.Processor
	logger    *slog.Logger

	// Auto-reject setting and mutex for thread safety
	autoRejectMu     sync.RWMutex
	autoRejectLarger bool
}

func (s *server) getfilebrowser(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	sort := r.URL.Query().Get("sort")

	// Default to name_asc if no sort parameter is provided
	if sort == "" {
		sort = "name_asc"
	}

	s.logger.Info("file browser request", "path", path, "sort", sort)

	err := elements.FilePicker(path, sort, s.queue()).Render(r.Context(), w)
	if err != nil {
		s.logger.Error("file browser render error", "path", path, "sort", sort, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) getfileinfo(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	s.logger.Info("file info request", "path", path)

	err := elements.FileInfo(path).Render(r.Context(), w)
	if err != nil {
		s.logger.Error("file info render error", "path", path, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) getstatus(w http.ResponseWriter, r *http.Request) {
	err := elements.Status(s.Processor.FFmpegBinary()).Render(r.Context(), w)
	if err != nil {
		s.logger.Error("cpu monitor render error", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) queue() []elements.TaskState {
	queue := []elements.TaskState{}
	for _, task := range s.Processor.GetQueue() {
		queue = append(queue, mapTaskState(task))
	}
	return queue
}

func mapTaskState(task processor.TaskState) elements.TaskState {
	errorMessage := ""
	if task.Error != nil {
		errorMessage = task.Error.Error()
	}

	return elements.TaskState{
		ID:        strconv.Itoa(int(task.ID)),
		Preset:    task.Preset,
		FileName:  path.Base(task.Input),
		Status:    task.Status,
		Progress:  task.Progress,
		InputFile: task.Input,
		TempFile:  task.TempFile,
		CreatedAt: task.CreateAt,
		Error:     errorMessage,
	}
}

func (s *server) getqueue(w http.ResponseWriter, r *http.Request) {
	queue := s.queue()
	slices.Reverse(queue)

	s.logger.Debug("queue request", "queue_length", len(queue))
	err := elements.Queue(queue).Render(r.Context(), w)
	if err != nil {
		s.logger.Error("queue render error", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Updated to use the specific VMAF template
func (s *server) getVMAF(w http.ResponseWriter, r *http.Request) {
	reference := r.URL.Query().Get("reference")
	distorted := r.URL.Query().Get("distorted")

	s.logger.Info("vmaf calculation request",
		"reference", reference,
		"distorted", distorted,
	)

	if reference == "" || distorted == "" {
		s.logger.Warn("missing vmaf parameters",
			"reference", reference,
			"distorted", distorted,
		)
		http.Error(w, "Missing 'reference' or 'distorted' parameter", http.StatusBadRequest)
		return
	}

	// Calculate VMAF score
	vmafScore, err := transcoding.CalculateVMAF(r.Context(), reference, distorted)
	if err != nil {
		s.logger.Error("vmaf calculation failed",
			"reference", reference,
			"distorted", distorted,
			"error", err,
		)
		http.Error(w, fmt.Sprintf("VMAF calculation error: %v", err), http.StatusInternalServerError)
		return
	}

	s.logger.Info("vmaf calculation complete",
		"reference", reference,
		"distorted", distorted,
		"score", vmafScore,
	)

	// Render VMAF score with the specific template
	err = pages.VMafScore(vmafScore).Render(r.Context(), w)
	if err != nil {
		s.logger.Error("vmaf render error", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// New handler for PSNR score
func (s *server) getPSNR(w http.ResponseWriter, r *http.Request) {
	reference := r.URL.Query().Get("reference")
	distorted := r.URL.Query().Get("distorted")

	s.logger.Info("psnr calculation request",
		"reference", reference,
		"distorted", distorted,
	)

	if reference == "" || distorted == "" {
		s.logger.Warn("missing psnr parameters",
			"reference", reference,
			"distorted", distorted,
		)
		http.Error(w, "Missing 'reference' or 'distorted' parameter", http.StatusBadRequest)
		return
	}

	// Calculate PSNR score
	psnrScore, err := transcoding.CalculatePSNR(r.Context(), reference, distorted)
	if err != nil {
		s.logger.Error("psnr calculation failed",
			"reference", reference,
			"distorted", distorted,
			"error", err,
		)
		http.Error(w, fmt.Sprintf("PSNR calculation error: %v", err), http.StatusInternalServerError)
		return
	}

	s.logger.Info("psnr calculation complete",
		"reference", reference,
		"distorted", distorted,
		"score", psnrScore,
	)

	// Render PSNR score with the specific template
	err = pages.PsnrScore(psnrScore).Render(r.Context(), w)
	if err != nil {
		s.logger.Error("psnr render error", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// New handler for SSIM score
func (s *server) getSSIM(w http.ResponseWriter, r *http.Request) {
	reference := r.URL.Query().Get("reference")
	distorted := r.URL.Query().Get("distorted")

	s.logger.Info("ssim calculation request",
		"reference", reference,
		"distorted", distorted,
	)

	if reference == "" || distorted == "" {
		s.logger.Warn("missing ssim parameters",
			"reference", reference,
			"distorted", distorted,
		)
		http.Error(w, "Missing 'reference' or 'distorted' parameter", http.StatusBadRequest)
		return
	}

	// Calculate SSIM score
	ssimScore, err := transcoding.CalculateSSIM(r.Context(), reference, distorted)
	if err != nil {
		s.logger.Error("ssim calculation failed",
			"reference", reference,
			"distorted", distorted,
			"error", err,
		)
		http.Error(w, fmt.Sprintf("SSIM calculation error: %v", err), http.StatusInternalServerError)
		return
	}

	s.logger.Info("ssim calculation complete",
		"reference", reference,
		"distorted", distorted,
		"score", ssimScore,
	)

	// Render SSIM score with the specific template
	err = pages.SsimScore(ssimScore).Render(r.Context(), w)
	if err != nil {
		s.logger.Error("ssim render error", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) pageResolver(w http.ResponseWriter, r *http.Request) {
	taskIdS := r.URL.Query().Get("taskid")
	taskId, err := strconv.Atoi(taskIdS)
	if err != nil {
		s.logger.Error("invalid task id", "task_id", taskIdS, "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if taskId == 0 {
		s.logger.Warn("task id not found", "task_id", taskId)
		http.Error(w, "Task ID not found", http.StatusNotFound)
		return
	}

	taskState := elements.TaskState{}

	// Retrieve the task and populate TaskState with rich information
	for _, task := range s.Processor.GetQueue() {
		if task.ID == uint64(taskId) {
			taskState = mapTaskState(task)
			break
		}
	}

	s.logger.Info("task resolver", "task_id", taskId, "status", taskState.Status)

	err = pages.Resolver(s.Processor.FFmpegBinary(), taskState).Render(r.Context(), w)
	if err != nil {
		s.logger.Error("resolver render error", "task_id", taskId, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) submitTask(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		s.logger.Error("parse form error", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filepath := r.FormValue("filepath")
	profileName := r.FormValue("profile")

	s.logger.Info("task submission", "filepath", filepath, "profile", profileName)

	s.Processor.AddTask(filepath, profileName)
}

func (s *server) submitTaskBatch(w http.ResponseWriter, r *http.Request) {
	log := s.logger.With("handler", "submitBatchTask")

	err := r.ParseForm()
	if err != nil {
		log.Error("parse form error", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	dir := r.FormValue("filepath")
	profileName := r.FormValue("profile")

	profile := s.Config.GetProfile(profileName)
	if profile == nil {
		log.Error("invalid profile", "profile", profileName)
		http.Error(w, "Invalid profile: "+profileName, http.StatusBadRequest)
		return
	}

	go func() {
		log.Info("processing batch task submission", "dir", dir, "profile", profileName)

		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			ext := strings.ToLower(filepath.Ext(path))

			if !slices.Contains(transcoding.VideoExtensions, ext) {
				return nil
			}

			if profile.BatchExcludeFilter != nil {
				matches, err := profile.BatchExcludeFilter.Matches(path)
				if err != nil {
					log.Error("error applying filter", "file", path, "error", err)
					return nil
				}
				if matches {
					log.Info("skipping file due to filter", "file", path)
					return nil
				}
			}

			if s.Processor.HasTask(path, profileName) {
				log.Info("skipping file, task already exists", "file", path, "profile", profileName)
				return nil
			}

			log.Info("adding file to queue", "file", path, "profile", profileName)
			s.Processor.AddTask(path, profileName)

			return nil
		})
		if err != nil {
			log.Error("error processing batch task", "error", err)
		}

	}()
}

func (s *server) submitTaskResolution(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	err := r.ParseForm()
	if err != nil {
		s.logger.Error("parse form error", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	taskIdS := r.FormValue("taskid")
	taskID, err := strconv.Atoi(taskIdS)
	if err != nil {
		s.logger.Error("invalid task id", "task_id", taskIdS, "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	replaceS := r.FormValue("replace")
	replace, err := strconv.ParseBool(replaceS)
	if err != nil {
		s.logger.Error("invalid replace value", "replace", replaceS, "error", err)
		http.Error(w, "Invalid value for 'replace' parameter: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.logger.Info("resolving task", "task_id", taskID, "replace", replace)

	s.Processor.ResolveTask(ctx, uint64(taskID), replace)

	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

func (s *server) submitTaskCancellation(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		s.logger.Error("parse form error", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	taskIdS := r.FormValue("taskid")
	taskId, err := strconv.Atoi(taskIdS)
	if err != nil {
		s.logger.Error("invalid task id", "task_id", taskIdS, "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.logger.Info("cancelling task", "task_id", taskId)

	err = s.Processor.CancelTask(uint64(taskId))
	if err != nil {
		s.logger.Error("task cancellation failed", "task_id", taskId, "error", err)
		http.Error(w, "Failed to cancel task: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func assetsRoutes(mux *http.ServeMux) {
	fs := http.FileServer(http.FS(assets.Assets))

	assetHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		fs.ServeHTTP(w, r)
	})

	mux.Handle("GET /assets/", http.StripPrefix("/assets/", assetHandler))
}

func (s *server) submitAutoRejectSetting(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		s.logger.Error("parse form error", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get the checkbox value - if present, it's checked (true), if absent, it's unchecked (false)
	autoReject := r.FormValue("auto-reject-larger") == "on"

	s.autoRejectMu.Lock()
	s.autoRejectLarger = autoReject
	s.autoRejectMu.Unlock()

	s.logger.Info("auto-reject setting updated", "enabled", autoReject)

	// If auto-reject is enabled, check all existing waiting_for_resolution tasks
	if autoReject {
		go s.processExistingWaitingTasks()
	}

	w.WriteHeader(http.StatusOK)
}

func (s *server) getAutoRejectSetting() bool {
	s.autoRejectMu.RLock()
	defer s.autoRejectMu.RUnlock()
	return s.autoRejectLarger
}

func (s *server) pageRoot(w http.ResponseWriter, r *http.Request) {
	err := pages.Root(s.Processor.FFmpegBinary(), s.Config.Profiles, s.queue(), s.getAutoRejectSetting()).Render(r.Context(), w)
	if err != nil {
		s.logger.Error("root page render error", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// handleTaskWaitingForResolution is called when a task transitions to waiting_for_resolution
// and handles auto-rejection if enabled and result file is larger than original
func (s *server) handleTaskWaitingForResolution(taskState processor.TaskState) {
	// Check if auto-reject is enabled
	if !s.getAutoRejectSetting() {
		return
	}

	log := s.logger.With("task_id", taskState.ID, "input", taskState.Input, "temp", taskState.TempFile)

	// Get file sizes
	originalSize, err := s.getFileSize(taskState.Input)
	if err != nil {
		log.Error("failed to get original file size for auto-reject", "error", err)
		return
	}

	resultSize, err := s.getFileSize(taskState.TempFile)
	if err != nil {
		log.Error("failed to get result file size for auto-reject", "error", err)
		return
	}

	log.Debug("auto-reject comparing file sizes", "original_size", originalSize, "result_size", resultSize)

	// If result file is larger than original, auto-reject
	if resultSize > originalSize {
		log.Info("auto-rejecting task due to larger result file",
			"original_size", originalSize, "result_size", resultSize,
			"size_diff", resultSize-originalSize)

		// Use context.Background since this is an automated action
		go s.Processor.ResolveTask(context.Background(), taskState.ID, false)
	} else {
		log.Debug("auto-reject: keeping task, result file is smaller or equal",
			"original_size", originalSize, "result_size", resultSize)
	}
}

// getFileSize returns the size of a file in bytes
func (s *server) getFileSize(filePath string) (int64, error) {
	if filePath == "" {
		return 0, fmt.Errorf("file path is empty")
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}

	return fileInfo.Size(), nil
}

// processExistingWaitingTasks scans all existing tasks in waiting_for_resolution state
// and applies auto-reject logic to them
func (s *server) processExistingWaitingTasks() {
	queue := s.Processor.GetQueue()
	waitingCount := 0

	for _, taskState := range queue {
		if taskState.Status == processor.TaskStatusWaitingForResolution {
			waitingCount++
			s.handleTaskWaitingForResolution(taskState)
		}
	}

	if waitingCount > 0 {
		s.logger.Info("processed existing waiting_for_resolution tasks for auto-reject", "count", waitingCount)
	}
}

// startPeriodicAutoRejectScanner runs a background process that periodically
// scans for waiting_for_resolution tasks when auto-reject is enabled
func (s *server) startPeriodicAutoRejectScanner() {
	ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	s.logger.Info("auto-reject scanner started")

	for range ticker.C {
		if s.getAutoRejectSetting() {
			s.logger.Debug("auto-reject scanner checking existing waiting tasks")
			s.processExistingWaitingTasks()
		}
	}
}
