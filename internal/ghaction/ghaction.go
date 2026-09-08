// Package ghaction reads an action.yml well enough to check it against
// itself. Actions resolves an undeclared reference to an empty string, so a
// deleted input leaves a step running with a blank argument rather than
// failing, and only a real job finds out.
package ghaction

import (
	"fmt"
	"os"
	"regexp"
	"sort"

	"go.yaml.in/yaml/v3"
)

// Action is the part of an action.yml worth checking: what it declares, and
// what its own body refers to.
type Action struct {
	Path   string
	Inputs map[string]yaml.Node `yaml:"inputs"`
	Runs   struct {
		Steps []struct {
			ID string `yaml:"id"`
		} `yaml:"steps"`
	} `yaml:"runs"`

	body string
}

var (
	inputRef = regexp.MustCompile(`inputs\.([A-Za-z0-9_-]+)`)
	stepRef  = regexp.MustCompile(`steps\.([A-Za-z0-9_-]+)\.outputs`)
)

// Load parses an action.yml.
func Load(path string) (*Action, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	a := &Action{Path: path, body: string(raw)}
	if err := yaml.Unmarshal(raw, a); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return a, nil
}

// UndeclaredInputs lists the inputs the body reads without declaring. Each one
// is an empty string at run time, which is the failure this catches.
func (a *Action) UndeclaredInputs() []string {
	return a.missing(inputRef, a.declaredInputs())
}

// UndeclaredSteps lists the step ids the body reads an output from without any
// step carrying that id.
func (a *Action) UndeclaredSteps() []string {
	declared := map[string]bool{}
	for _, s := range a.Runs.Steps {
		if s.ID != "" {
			declared[s.ID] = true
		}
	}
	return a.missing(stepRef, declared)
}

// UnusedInputs lists the inputs nothing reads, which is a knob that does
// nothing rather than a break.
func (a *Action) UnusedInputs() []string {
	read := map[string]bool{}
	for _, m := range inputRef.FindAllStringSubmatch(a.body, -1) {
		read[m[1]] = true
	}
	var out []string
	for name := range a.Inputs {
		if !read[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func (a *Action) declaredInputs() map[string]bool {
	declared := map[string]bool{}
	for name := range a.Inputs {
		declared[name] = true
	}
	return declared
}

func (a *Action) missing(re *regexp.Regexp, declared map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllStringSubmatch(a.body, -1) {
		if name := m[1]; !declared[name] && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
