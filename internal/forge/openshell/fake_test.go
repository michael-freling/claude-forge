package openshell

import (
	"context"
	"errors"
	"os"
	"strings"
)

// call is one recorded openshell invocation.
type call struct {
	Env         []string
	Args        []string
	Interactive bool
	File        string // contents of the -f file, read when the call ran
}

func (c call) String() string { return strings.Join(c.Args, " ") }

// fakeRunner answers openshell invocations by argument prefix.
type fakeRunner struct {
	calls     []call
	responses map[string]string // prefix -> output
	failures  map[string]error  // prefix -> error
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{responses: map[string]string{}, failures: map[string]error{}}
}

func (f *fakeRunner) match(args []string) (string, error) {
	joined := strings.Join(args, " ")
	best, bestLen := "", -1
	for prefix := range f.failures {
		if strings.HasPrefix(joined, prefix) && len(prefix) > bestLen {
			best, bestLen = prefix, len(prefix)
		}
	}
	if bestLen >= 0 {
		return "", f.failures[best]
	}
	for prefix, out := range f.responses {
		if strings.HasPrefix(joined, prefix) && len(prefix) > bestLen {
			best, bestLen = out, len(prefix)
		}
	}
	return best, nil
}

func (f *fakeRunner) Output(_ context.Context, env []string, args ...string) ([]byte, error) {
	c := call{Env: env, Args: args}
	for i, a := range args {
		if a == "-f" && i+1 < len(args) {
			data, _ := os.ReadFile(args[i+1])
			c.File = string(data)
		}
	}
	f.calls = append(f.calls, c)
	out, err := f.match(args)
	return []byte(out), err
}

func (f *fakeRunner) Interactive(_ context.Context, args ...string) error {
	f.calls = append(f.calls, call{Args: args, Interactive: true})
	_, err := f.match(args)
	return err
}

// find returns the recorded calls whose args start with prefix.
func (f *fakeRunner) find(prefix string) []call {
	var out []call
	for _, c := range f.calls {
		if strings.HasPrefix(c.String(), prefix) {
			out = append(out, c)
		}
	}
	return out
}

var errFake = errors.New("fake failure")

func sandboxListJSON(entries ...string) string {
	return `{"sandboxes":[` + strings.Join(entries, ",") + `]}`
}

func sandboxJSON(name, phase string, forge bool) string {
	labels := `{}`
	if forge {
		labels = `{"claude-forge":"true"}`
	}
	return `{"name":"` + name + `","phase":"` + phase + `","created_at":"2026-11-22 03:40:00","labels":` + labels + `}`
}
