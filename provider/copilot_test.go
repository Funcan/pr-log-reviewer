package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCopilot_ExchangeAndComplete(t *testing.T) {
	var tokenCalls int32

	mux := http.NewServeMux()
	var apiBase string // set after server starts so the token response can advertise it

	mux.HandleFunc("/copilot_internal/v2/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenCalls, 1)
		if got := r.Header.Get("Authorization"); got != "token gho_oauth" {
			t.Errorf("token-exchange auth = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(copilotTokenResponse{
			Token:     "tid=copilot-session",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
			Endpoints: struct {
				API string `json:"api"`
			}{API: apiBase},
		})
	})

	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tid=copilot-session" {
			t.Errorf("chat auth = %q", got)
		}
		if got := r.Header.Get("Copilot-Integration-Id"); got != "vscode-chat" {
			t.Errorf("integration id = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		var req openAIChatRequest
		_ = json.Unmarshal(body, &req)
		if req.Model != "gpt-4o" {
			t.Errorf("model = %q", req.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"model": "gpt-4o-2024-11-20",
			"choices": [{"message": {"content": "Hi there, friend!"}}],
			"usage": {"prompt_tokens": 14, "completion_tokens": 6}
		}`)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	apiBase = srv.URL

	p := NewCopilot("gho_oauth", "gpt-4o",
		WithCopilotTokenURL(srv.URL+"/copilot_internal/v2/token"))

	for i := 0; i < 2; i++ {
		resp, err := p.Complete(context.Background(), Request{
			Messages: []Message{{Role: RoleUser, Content: "Say hi"}},
		})
		if err != nil {
			t.Fatalf("Complete #%d: %v", i, err)
		}
		if resp.Content != "Hi there, friend!" {
			t.Errorf("content = %q", resp.Content)
		}
		if resp.Model != "gpt-4o-2024-11-20" {
			t.Errorf("model = %q", resp.Model)
		}
	}

	// The session token must be cached and reused across requests.
	if n := atomic.LoadInt32(&tokenCalls); n != 1 {
		t.Errorf("token exchanged %d times, want 1 (should be cached)", n)
	}
}

func TestCopilot_RefreshesExpiredToken(t *testing.T) {
	var tokenCalls int32
	var apiBase string

	mux := http.NewServeMux()
	mux.HandleFunc("/copilot_internal/v2/token", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&tokenCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		// First token already expired; forces a refresh on the next call.
		exp := time.Now().Add(-time.Minute)
		if n > 1 {
			exp = time.Now().Add(time.Hour)
		}
		_, _ = fmt.Fprintf(w, `{"token":"tid=t%d","expires_at":%d,"endpoints":{"api":%q}}`,
			n, exp.Unix(), apiBase)
	})
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"m","choices":[{"message":{"content":"ok"}}],"usage":{}}`)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	apiBase = srv.URL

	p := NewCopilot("gho_oauth", "m", WithCopilotTokenURL(srv.URL+"/copilot_internal/v2/token"))
	for i := 0; i < 2; i++ {
		if _, err := p.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "x"}}}); err != nil {
			t.Fatalf("Complete #%d: %v", i, err)
		}
	}
	if n := atomic.LoadInt32(&tokenCalls); n != 2 {
		t.Errorf("token exchanged %d times, want 2 (expired token should refresh)", n)
	}
}
