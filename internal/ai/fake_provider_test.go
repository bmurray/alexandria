package ai

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// fakeProvider replays scripted responses and records every request.
type fakeProvider struct {
	mu       sync.Mutex
	name     string
	model    string
	script   []scripted
	Requests []Request
}

type scripted struct {
	resp *Response
	err  error
}

func text(t string, in, out int) scripted {
	return scripted{resp: &Response{Text: t, StopReason: StopEndTurn, Usage: Usage{InputTokens: in, OutputTokens: out}}}
}

func toolUse(name, input string, in, out int) scripted {
	return scripted{resp: &Response{
		StopReason: StopToolUse,
		ToolUses:   []ToolUse{{ID: "tu_" + name, Name: name, Input: json.RawMessage(input)}},
		Usage:      Usage{InputTokens: in, OutputTokens: out},
	}}
}

func failing(msg string) scripted { return scripted{err: errors.New(msg)} }

func newFake(name string, script ...scripted) *fakeProvider {
	return &fakeProvider{name: name, model: name + "-model", script: script}
}

func (f *fakeProvider) Name() string  { return f.name }
func (f *fakeProvider) Model() string { return f.model }

func (f *fakeProvider) Complete(_ context.Context, req Request) (*Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Requests = append(f.Requests, req)
	if len(f.script) == 0 {
		return nil, errors.New("fake provider: script exhausted")
	}
	s := f.script[0]
	f.script = f.script[1:]
	if s.err != nil {
		return nil, s.err
	}
	cp := *s.resp
	return &cp, nil
}

func (f *fakeProvider) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Requests)
}
