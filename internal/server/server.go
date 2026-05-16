package server

import (
	"log"
	"net/http"
	"os"

	"github.com/wow-look-at-my/log-streamer/internal/storage"
)

type Config struct {
	Addr    string
	DataDir string
}

func ConfigFromEnv() Config {
	cfg := Config{
		Addr:    ":8080",
		DataDir: "./data",
	}
	if v := os.Getenv("LOG_STREAMER_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("LOG_STREAMER_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	return cfg
}

type Server struct {
	config Config
	store  *storage.Store
	mux    *http.ServeMux
}

func New(cfg Config) (*Server, error) {
	store, err := storage.New(cfg.DataDir)
	if err != nil {
		return nil, err
	}

	s := &Server{
		config: cfg,
		store:  store,
		mux:    http.NewServeMux(),
	}

	s.mux.HandleFunc("GET /api/stream", s.handleStream)
	s.mux.HandleFunc("GET /api/logs/{token}", s.handleFetch)
	s.mux.HandleFunc("DELETE /api/logs/{token}", s.handleDelete)

	return s, nil
}

func (s *Server) Run() error {
	log.Printf("listening on %s, data dir: %s", s.config.Addr, s.config.DataDir)
	return http.ListenAndServe(s.config.Addr, s.mux)
}
