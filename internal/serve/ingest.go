package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/wm-it-25/git-branch-graph/internal/pipeline"
)

// sseEvent is one progress frame streamed to the client.
type sseEvent struct {
	Stage string `json:"stage"`
	Pct   int    `json:"pct"`
	Msg   string `json:"msg"`
	Done  bool   `json:"done,omitempty"`
	Error string `json:"error,omitempty"`
	RunID string `json:"runId,omitempty"`
}

type ingestJob struct {
	ch chan sseEvent
}

var (
	jobs   sync.Map // jobID -> *ingestJob
	jobSeq atomic.Int64
)

// handleIngestStart kicks off a pipeline run and returns a job id the client
// subscribes to for progress.
func (s *Server) handleIngestStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Input == "" {
		httpErr(w, http.StatusBadRequest, fmt.Errorf("body must be {\"input\": \"…\"}"))
		return
	}

	id := "job" + strconv.FormatInt(jobSeq.Add(1), 10)
	job := &ingestJob{ch: make(chan sseEvent, 64)}
	jobs.Store(id, job)

	go func() {
		defer close(job.ch)
		res, err := pipeline.Run(pipeline.Options{Input: body.Input, DataDir: s.DataDir},
			func(stage string, pct int, msg string) {
				select {
				case job.ch <- sseEvent{Stage: stage, Pct: pct, Msg: msg}:
				default: // drop if the buffer is full; the bar still advances
				}
			})
		if err != nil {
			job.ch <- sseEvent{Done: true, Error: err.Error()}
			return
		}
		runID, regErr := s.registerRun(res)
		if regErr != nil {
			job.ch <- sseEvent{Done: true, Error: regErr.Error()}
			return
		}
		job.ch <- sseEvent{Stage: "done", Pct: 100, Msg: "Ready", Done: true, RunID: runID}
	}()

	writeJSON(w, map[string]string{"jobId": id})
}

// registerRun makes the produced run servable by id. Runs outside the data dir
// (an externally-supplied already-analyzed folder) are symlinked in.
func (s *Server) registerRun(res pipeline.Result) (string, error) {
	absData, _ := filepath.Abs(s.DataDir)
	absRun, _ := filepath.Abs(res.RunDir)
	if filepath.Dir(absRun) == absData {
		return filepath.Base(absRun), nil // already a direct child
	}
	link := filepath.Join(s.DataDir, res.RunID)
	if existing, err := os.Readlink(link); err == nil && existing == absRun {
		return res.RunID, nil // already linked
	}
	_ = os.Remove(link)
	if err := os.Symlink(absRun, link); err != nil {
		return "", fmt.Errorf("register external run: %w", err)
	}
	return res.RunID, nil
}

// handleIngestEvents streams a job's progress as Server-Sent Events.
func (s *Server) handleIngestEvents(w http.ResponseWriter, r *http.Request) {
	v, ok := jobs.Load(r.PathValue("jobId"))
	if !ok {
		httpErr(w, http.StatusNotFound, fmt.Errorf("unknown job"))
		return
	}
	job := v.(*ingestJob)
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpErr(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, open := <-job.ch:
			if !open {
				jobs.Delete(r.PathValue("jobId"))
				return
			}
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
			if ev.Done {
				jobs.Delete(r.PathValue("jobId"))
				return
			}
		}
	}
}
