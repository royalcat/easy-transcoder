package main

import (
	"fmt"
	"net/http"
	"path"
	"strconv"

	"github.com/a-h/templ"

	"github.com/royalcat/easy-transcode/assets"
	"github.com/royalcat/easy-transcode/internal/config"
	"github.com/royalcat/easy-transcode/internal/processor"
	"github.com/royalcat/easy-transcode/internal/profile"
	"github.com/royalcat/easy-transcode/ui/elements"
	"github.com/royalcat/easy-transcode/ui/pages"
)

func main() {
	mux := http.NewServeMux()

	assetsRoutes(mux)

	config := config.Config{
		Profiles: []profile.Profile{
			{
				Name: "H264",
				Params: map[string]string{
					"c:v":    "libx264",
					"preset": "ultrafast",
					"c:a":    "copy",
				},
			},
		},
	}

	q := processor.NewQueue(config)
	q.Start()

	s := &server{
		Config: config,
		Queue:  q,
	}

	templHandler := func(c templ.Component) http.Handler {
		return templ.Handler(c,
			templ.WithErrorHandler(func(r *http.Request, err error) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				})
			}),
		)
	}

	mux.Handle("GET /", templHandler(pages.Root(config.Profiles)))
	mux.Handle("GET /resolver", http.HandlerFunc(s.pageResolver))
	mux.Handle("GET /create-task", templHandler(pages.TaskCreation(config.Profiles)))

	mux.Handle("GET /elements/filepicker", http.HandlerFunc(getfilebrowser))
	mux.Handle("GET /elements/fileinfo", http.HandlerFunc(getfileinfo))
	mux.Handle("GET /elements/queue", http.HandlerFunc(s.getqueue))

	mux.Handle("POST /submit/task", http.HandlerFunc(s.submitTask))
	mux.Handle("POST /submit/resolve", http.HandlerFunc(s.submitTaskResolution))
	// mux.Handle("GET /elements/profileselector", http.HandlerFunc(s.getprofile))

	fmt.Println("Listening on :8080")
	http.ListenAndServe(":8080", mux)
}

type server struct {
	Config config.Config
	Queue  *processor.Processor
}

func getfilebrowser(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	err := elements.FilePicker(path).Render(r.Context(), w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func getfileinfo(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	err := elements.FileInfo(path).Render(r.Context(), w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) getqueue(w http.ResponseWriter, r *http.Request) {
	queue := []elements.TaskState{}

	for _, task := range s.Queue.GetQueue() {
		queue = append(queue, elements.TaskState{
			ID:       strconv.Itoa(int(task.ID)),
			Preset:   task.Preset,
			FileName: path.Base(task.Input),
			Status:   task.Status,
			Progress: task.Progress,

			InputFile: task.Input,
			TempFile:  task.TempFile,
		})
	}

	err := elements.Queue(queue).Render(r.Context(), w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) pageResolver(w http.ResponseWriter, r *http.Request) {
	taskIdS := r.URL.Query().Get("taskid")
	taskId, err := strconv.Atoi(taskIdS)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if taskId == 0 {
		http.Error(w, "Task ID not found", http.StatusNotFound)
		return
	}

	taskState := elements.TaskState{}

	for _, task := range s.Queue.GetQueue() {
		if task.ID == uint64(taskId) {
			taskState = elements.TaskState{
				ID:       strconv.Itoa(int(task.ID)),
				Preset:   task.Preset,
				FileName: path.Base(task.Input),
				Status:   task.Status,
				Progress: task.Progress,

				InputFile: task.Input,
				TempFile:  task.TempFile,
			}
			break
		}
	}

	err = pages.Resolver(taskState).Render(r.Context(), w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *server) submitTask(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	filepath := r.FormValue("filepath")
	profileName := r.FormValue("profile")

	fmt.Printf("Submitting task for file: %s with profile: %s\n", filepath, profileName)
	s.Queue.AddTask(filepath, profileName)
}

func (s *server) submitTaskResolution(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	taskIdS := r.FormValue("taskid")
	taskId, err := strconv.Atoi(taskIdS)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	replaceS := r.FormValue("replace")
	replace, err := strconv.ParseBool(replaceS)
	if err != nil {
		http.Error(w, "Invalid value for 'replace' parameter: "+err.Error(), http.StatusBadRequest)
		return
	}

	task := s.Queue.GetTask(uint64(taskId))

	s.Queue.ResolveTask(task, replace)

	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

// func (s *server) getprofile(w http.ResponseWriter, r *http.Request) {
// 	profileName := r.URL.Query().Get("profile")

// 	profile := profile.Profile{}
// 	for _, p := range s.Config.Profiles {
// 		if p.Name == profileName {
// 			profile = p
// 			break
// 		}
// 	}

// 	err := elements.ProfileSelector(profile).Render(r.Context(), w)
// 	if err != nil {
// 		http.Error(w, err.Error(), http.StatusInternalServerError)
// 		return
// 	}
// }

func assetsRoutes(mux *http.ServeMux) {
	var isDevelopment = true

	assetHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isDevelopment {
			w.Header().Set("Cache-Control", "no-store")
		}

		var fs http.Handler
		if isDevelopment {
			fs = http.FileServer(http.Dir("../assets"))
		} else {
			fs = http.FileServer(http.FS(assets.Assets))
		}

		fs.ServeHTTP(w, r)
	})

	mux.Handle("GET /assets/", http.StripPrefix("/assets/", assetHandler))
}
