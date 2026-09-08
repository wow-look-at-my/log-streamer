package protocol

import (
	"encoding/json"
	"strings"
)

// Marker event names.
const (
	EventStepStart = "step_start"
	EventStepEnd   = "step_end"
)

// Marker is a step boundary. A whole job shares a token, so markers are what
// split that log back into steps.
type Marker struct {
	Event    string `json:"event"`
	Step     string `json:"step,omitempty"`
	Name     string `json:"name,omitempty"`
	Job      string `json:"job,omitempty"`
	Workflow string `json:"workflow,omitempty"`

	// Cmd is the command the step opens with. Actions exports no step name, so
	// this is what makes a section recognizable when nobody labelled it.
	Cmd string `json:"cmd,omitempty"`

	// Exit is the step's status, carried by an end marker only.
	Exit *int `json:"exit,omitempty"`
}

// EncodeMarker renders a marker as the line it occupies in the stream.
func EncodeMarker(m Marker) ([]byte, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ParseMarker reads a line from the marker stream. It reports false for a line
// that is not a marker, so a writer of an older version cannot break a reader.
func ParseMarker(line string) (Marker, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") {
		return Marker{}, false
	}
	var m Marker
	if json.Unmarshal([]byte(line), &m) != nil {
		return Marker{}, false
	}
	if m.Event != EventStepStart && m.Event != EventStepEnd {
		return Marker{}, false
	}
	return m, true
}

// Label names a step for a reader, preferring the name its author gave it,
// then the command it ran, then the runner's step id.
func (m Marker) Label() string {
	switch {
	case m.Name != "":
		return m.Name
	case m.Cmd != "":
		return m.Cmd
	case m.Step != "":
		return m.Step
	}
	return "(unnamed)"
}
