package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/wow-look-at-my/log-streamer/internal/protocol"
	"github.com/wow-look-at-my/log-streamer/internal/token"
)

func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	if !token.Validate(tok) {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorResponse{Error: "invalid token format"})
		return
	}

	since, err := parseSince(r.URL.Query().Get("since"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorResponse{Error: err.Error()})
		return
	}

	lines, err := s.store.Fetch(tok)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, protocol.ErrorResponse{Error: "token not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, protocol.ErrorResponse{Error: "failed to read logs"})
		return
	}

	// Count stays the total, so a follower can spot a log that shrank and rewind.
	total := len(lines)
	if since > total {
		since = total
	}

	writeJSON(w, http.StatusOK, protocol.FetchResponse{
		Token: tok,
		Lines: lines[since:],
		Count: total,
	})
}

// handleGroup lists the streams that registered under a group token. That
// token reaches every log it names, so it is as much a secret as they are.
func (s *Server) handleGroup(w http.ResponseWriter, r *http.Request) {
	group := r.PathValue("group")
	if !token.Validate(group) {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorResponse{Error: "invalid token format"})
		return
	}

	members, err := s.store.Members(group)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, protocol.ErrorResponse{Error: "group not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, protocol.ErrorResponse{Error: "failed to read the group"})
		return
	}

	writeJSON(w, http.StatusOK, protocol.GroupResponse{Group: group, Streams: members})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	if !token.Validate(tok) {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorResponse{Error: "invalid token format"})
		return
	}

	if err := s.store.Delete(tok); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, protocol.ErrorResponse{Error: "token not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, protocol.ErrorResponse{Error: "failed to delete logs"})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// parseSince reads the line offset a follower resumes from. Empty means the
// whole log.
func parseSince(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, errors.New("since must be a non-negative integer")
	}
	return n, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
