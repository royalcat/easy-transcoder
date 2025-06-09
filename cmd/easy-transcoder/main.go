package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"
	"time"

	"github.com/a-h/templ"

	"github.com/royalcat/easy-transcoder/assets"
	"github.com/royalcat/easy-transcoder/internal/config"
	"github.com/royalcat/easy-transcoder/internal/processor"
	"github.com/royalcat/easy-transcoder/internal/transcoding"
	"github.com/royalcat/easy-transcoder/ui/elements"
	"github.com/royalcat/easy-transcoder/ui/modules"
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
		Config: cfg,
		Queue:  q,
		logger: logger,
	}

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

	mux.Handle("GET /", templHandler(pages.Root(cfg.Profiles, s.queue())))
	mux.Handle("GET /resolver", http.HandlerFunc(s.pageResolver))
	mux.Handle("GET /create-task", templHandler(pages.TaskCreation(cfg.Profiles, s.queue())))

	mux.Handle("GET /elements/filepicker", http.HandlerFunc(s.getfilebrowser))
	mux.Handle("GET /elements/fileinfo", http.HandlerFunc(s.getfileinfo))
	mux.Handle("GET /elements/queue", http.HandlerFunc(s.getqueue))
	mux.Handle("GET /elements/cpumonitor", http.HandlerFunc(s.getcpumonitor))

	// Replaced the single VMAF endpoint with three separate metric endpoints
	mux.Handle("GET /metrics/vmaf", http.HandlerFunc(s.getVMAF))
	mux.Handle("GET /metrics/psnr", http.HandlerFunc(s.getPSNR))
	mux.Handle("GET /metrics/ssim", http.HandlerFunc(s.getSSIM))

	mux.Handle("POST /submit/task", http.HandlerFunc(s.submitTask))
	mux.Handle("POST /submit/resolve", http.HandlerFunc(s.submitTaskResolution))
	mux.Handle("POST /submit/cancel", http.HandlerFunc(s.submitTaskCancellation))

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
	Config config.Config
	Queue  *processor.Processor
	logger *slog.Logger
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

func (s *server) getcpumonitor(w http.ResponseWriter, r *http.Request) {
	err := modules.CPUMonitor().Render(r.Context(), w)
	if err != nil {
		s.logger.Error("cpu monitor render error", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) queue() []elements.TaskState {
	queue := []elements.TaskState{}
	for _, task := range s.Queue.GetQueue() {
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
	for _, task := range s.Queue.GetQueue() {
		if task.ID == uint64(taskId) {
			taskState = mapTaskState(task)
			break
		}
	}

	s.logger.Info("task resolver", "task_id", taskId, "status", taskState.Status)

	err = pages.Resolver(taskState).Render(r.Context(), w)
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

	s.Queue.AddTask(filepath, profileName)
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

	s.Queue.ResolveTask(ctx, uint64(taskID), replace)

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

	err = s.Queue.CancelTask(uint64(taskId))
	if err != nil {
		s.logger.Error("task cancellation failed", "task_id", taskId, "error", err)
		http.Error(w, "Failed to cancel task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Refresh the queue after cancellation
	s.getqueue(w, r)
}

func assetsRoutes(mux *http.ServeMux) {
	fs := http.FileServer(http.FS(assets.Assets))

	assetHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		fs.ServeHTTP(w, r)
	})

	mux.Handle("GET /assets/", http.StripPrefix("/assets/", assetHandler))
}
