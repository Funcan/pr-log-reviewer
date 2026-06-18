package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// cassette is the on-disk record of a single completion: request plus response.
// It is stored as pretty JSON so fixtures are reviewable in diffs.
type cassette struct {
	Provider string   `json:"provider"`
	Model    string   `json:"model"`
	Request  Request  `json:"request"`
	Response Response `json:"response"`
}

// fixtureKey derives a stable filename from the provider, model, and request.
// Any change to prompt text, parameters, or model produces a new key, which is
// what surfaces fixture drift when prompts change.
func fixtureKey(name, model string, req Request) string {
	h := sha256.New()
	enc := json.NewEncoder(h)
	_ = enc.Encode(struct {
		Provider string  `json:"provider"`
		Model    string  `json:"model"`
		Request  Request `json:"request"`
	}{name, model, req})
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// Replayer serves recorded responses from a fixture directory and never makes
// network calls. It is the provider used in CI for deterministic eval runs.
type Replayer struct {
	dir   string
	name  string
	model string
}

// NewReplayer returns a Replayer reading fixtures from dir. name/model are used
// for keying and must match the provider/model that recorded the fixtures.
func NewReplayer(dir, name, model string) *Replayer {
	return &Replayer{dir: dir, name: name, model: model}
}

func (r *Replayer) Name() string  { return r.name }
func (r *Replayer) Model() string { return r.model }

func (r *Replayer) Complete(_ context.Context, req Request) (Response, error) {
	key := fixtureKey(r.name, r.model, req)
	path := filepath.Join(r.dir, key+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Response{}, fmt.Errorf("replay: no fixture for key %s (record it with a live run)", key)
		}
		return Response{}, fmt.Errorf("replay: read fixture: %w", err)
	}
	var c cassette
	if err := json.Unmarshal(data, &c); err != nil {
		return Response{}, fmt.Errorf("replay: decode fixture %s: %w", key, err)
	}
	return c.Response, nil
}

// Recorder wraps a live Provider, persisting every completion to a fixture
// directory so the interaction can later be replayed deterministically.
type Recorder struct {
	inner Provider
	dir   string
}

// NewRecorder wraps inner, writing fixtures into dir (created if absent).
func NewRecorder(inner Provider, dir string) *Recorder {
	return &Recorder{inner: inner, dir: dir}
}

func (r *Recorder) Name() string  { return r.inner.Name() }
func (r *Recorder) Model() string { return r.inner.Model() }

func (r *Recorder) Complete(ctx context.Context, req Request) (Response, error) {
	resp, err := r.inner.Complete(ctx, req)
	if err != nil {
		return resp, err
	}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return resp, fmt.Errorf("record: mkdir: %w", err)
	}
	c := cassette{
		Provider: r.inner.Name(),
		Model:    r.inner.Model(),
		Request:  req,
		Response: resp,
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return resp, fmt.Errorf("record: marshal: %w", err)
	}
	key := fixtureKey(r.inner.Name(), r.inner.Model(), req)
	path := filepath.Join(r.dir, key+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return resp, fmt.Errorf("record: write fixture: %w", err)
	}
	return resp, nil
}
