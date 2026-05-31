package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wow-look-at-my/log-streamer/internal/storage"
)

const (
	defaultMaxStreamBytes = 1 << 30 // 1 GiB
	defaultMaxTotalBytes  = 0       // unlimited
	defaultTTL            = 7 * 24 * time.Hour
	defaultSweepInterval  = 10 * time.Minute
)

type Config struct {
	Addr           string
	DataDir        string
	MaxStreamBytes int64
	MaxTotalBytes  int64
	TTL            time.Duration
	SweepInterval  time.Duration
}

func ConfigFromEnv() Config {
	cfg := Config{
		Addr:           ":8080",
		DataDir:        "./data",
		MaxStreamBytes: defaultMaxStreamBytes,
		MaxTotalBytes:  defaultMaxTotalBytes,
		TTL:            defaultTTL,
		SweepInterval:  defaultSweepInterval,
	}
	if v := os.Getenv("LOG_STREAMER_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("LOG_STREAMER_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("LOG_STREAMER_MAX_STREAM_BYTES"); v != "" {
		cfg.MaxStreamBytes = parseBytes(v, cfg.MaxStreamBytes)
	}
	if v := os.Getenv("LOG_STREAMER_MAX_TOTAL_BYTES"); v != "" {
		cfg.MaxTotalBytes = parseBytes(v, cfg.MaxTotalBytes)
	}
	if v := os.Getenv("LOG_STREAMER_TTL"); v != "" {
		cfg.TTL = parseDuration(v, cfg.TTL)
	}
	if v := os.Getenv("LOG_STREAMER_SWEEP_INTERVAL"); v != "" {
		cfg.SweepInterval = parseDuration(v, cfg.SweepInterval)
	}
	return cfg
}

func parseBytes(s string, def int64) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n < 0 {
		log.Printf("invalid byte limit %q, using default %d", s, def)
		return def
	}
	return n
}

func parseDuration(s string, def time.Duration) time.Duration {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil || d < 0 {
		log.Printf("invalid duration %q, using default %s", s, def)
		return def
	}
	return d
}

type Server struct {
	config Config
	store  *storage.Store
	mux    *http.ServeMux
}

func New(cfg Config) (*Server, error) {
	store, err := storage.New(storage.Options{
		Dir:            cfg.DataDir,
		MaxStreamBytes: cfg.MaxStreamBytes,
		MaxTotalBytes:  cfg.MaxTotalBytes,
		TTL:            cfg.TTL,
	})
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

// Handler returns the server's HTTP handler, primarily for tests.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) Run() error {
	srv := &http.Server{
		Addr:    s.config.Addr,
		Handler: s.mux,
		// Bound the time a slow client may take to send request headers, so an
		// idle/slowloris connection cannot tie up a connection indefinitely. No
		// WriteTimeout: it would kill long-lived streaming WebSocket connections.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if s.config.TTL > 0 && s.config.SweepInterval > 0 {
		go s.runSweeper(ctx)
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	log.Printf("listening on %s, data dir: %s (per-stream cap: %d bytes, total cap: %d bytes, ttl: %s)",
		s.config.Addr, s.config.DataDir, s.config.MaxStreamBytes, s.config.MaxTotalBytes, s.config.TTL)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) runSweeper(ctx context.Context) {
	sweep := func() {
		if n, err := s.store.Sweep(time.Now()); err != nil {
			log.Printf("sweep: %v", err)
		} else if n > 0 {
			log.Printf("sweep: removed %d expired stream(s)", n)
		}
	}
	sweep()
	ticker := time.NewTicker(s.config.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}
