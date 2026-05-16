package server

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/wow-look-at-my/log-streamer/internal/protocol"
	"github.com/wow-look-at-my/log-streamer/internal/token"
)

func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	if !token.Validate(tok) {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorResponse{Error: "invalid token format"})
		return
	}
	if !s.store.Exists(tok) {
		writeJSON(w, http.StatusNotFound, protocol.ErrorResponse{Error: "token not found"})
		return
	}

	lines, err := s.store.Fetch(tok)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, protocol.ErrorResponse{Error: "failed to read logs"})
		return
	}

	writeJSON(w, http.StatusOK, protocol.FetchResponse{
		Token: tok,
		Lines: lines,
		Count: len(lines),
	})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	if !token.Validate(tok) {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorResponse{Error: "invalid token format"})
		return
	}

	err := s.store.Delete(tok)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, protocol.ErrorResponse{Error: "token not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, protocol.ErrorResponse{Error: "failed to delete logs"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
