// internal/server/server.go
package server

import (
	"io/fs"
	"log"
	"net/http"
)

type Server struct {
	handlers *Handlers
	uiFS     fs.FS // nil until UI is embedded
}

func New(h *Handlers, uiFS fs.FS) *Server {
	return &Server{handlers: h, uiFS: uiFS}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", s.handlers.Status)
	mux.HandleFunc("/api/history", s.handlers.History)
	mux.HandleFunc("/api/config", s.handlers.Config)
	mux.HandleFunc("/api/silences", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.handlers.Silences(w, r)
		case http.MethodPost:
			s.handlers.CreateSilence(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/silences/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			s.handlers.DeleteSilence(w, r)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	if s.uiFS != nil {
		fileServer := http.FileServer(http.FS(s.uiFS))
		mux.Handle("/", spaHandler{fileServer: fileServer, uiFS: s.uiFS})
	}

	return mux
}

func (s *Server) ListenAndServe(addr string) error {
	log.Printf("server: listening on %s", addr)
	return http.ListenAndServe(addr, s.Handler())
}

// spaHandler serves static files and falls back to index.html for SPA routing.
type spaHandler struct {
	fileServer http.Handler
	uiFS       fs.FS
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, err := h.uiFS.Open(r.URL.Path)
	if err != nil {
		// File not found — serve index.html for client-side routing
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		h.fileServer.ServeHTTP(w, r2)
		return
	}
	h.fileServer.ServeHTTP(w, r)
}
