package provider

import "testing"

func TestBuild_Validation(t *testing.T) {
	if _, err := Build(Config{Provider: "local"}); err == nil {
		t.Error("expected error when model is missing")
	}
	if _, err := Build(Config{Provider: "nope", Model: "m"}); err == nil {
		t.Error("expected error for unknown provider kind")
	}
	if _, err := Build(Config{Provider: "anthropic", Model: "m", APIKey: ""}); err == nil {
		t.Setenv("ANTHROPIC_API_KEY", "")
		t.Error("expected error when anthropic key missing")
	}
}

func TestBuild_ResolvesKinds(t *testing.T) {
	t.Setenv("GITHUB_MODELS_TOKEN", "tok")
	t.Setenv("ANTHROPIC_API_KEY", "akey")

	cases := []struct {
		cfg      Config
		wantName string
	}{
		{Config{Provider: "github-models", Model: "openai/gpt-4o"}, "github-models"},
		{Config{Provider: "copilot", Model: "gpt-4o", APIKey: "gho_fake"}, "copilot"},
		{Config{Provider: "claude", Model: "claude-3-5-sonnet"}, "anthropic"},
		{Config{Provider: "gemini", Model: "gemini-2.0-flash", APIKey: "g"}, "gemini"},
		{Config{Provider: "local", Model: "llama3"}, "local"},
		{Config{Provider: "openai", Model: "gpt-4o", APIKey: "x"}, "openai"},
	}
	for _, tc := range cases {
		p, err := Build(tc.cfg)
		if err != nil {
			t.Fatalf("Build(%+v): %v", tc.cfg, err)
		}
		if p.Name() != tc.wantName {
			t.Errorf("Build(%+v).Name() = %q, want %q", tc.cfg, p.Name(), tc.wantName)
		}
		if p.Model() != tc.cfg.Model {
			t.Errorf("Build(%+v).Model() = %q, want %q", tc.cfg, p.Model(), tc.cfg.Model)
		}
	}
}
