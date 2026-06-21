// Command eval runs the reviewer over a corpus of changes paired with good and
// bad messages, and reports reliability metrics (pairwise discrimination,
// absolute thresholds, and defect recall). It exits non-zero when the metrics
// fall below their targets.
//
// Modes:
//
//	replay : use recorded fixtures only; no network (the default, for CI)
//	record : call the live provider and persist fixtures for later replay
//	live   : call the live provider without recording
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"pr-log-reviewer/eval"
	"pr-log-reviewer/provider"
	"pr-log-reviewer/review"
)

type options struct {
	corpus  string
	mode    string
	caseID  string
	fixture string

	providerKind string
	model        string
	baseURL      string
	apiKey       string

	temperature float64
	maxTokens   int
	maxRetries  int

	goodMin int
	okayMin int
	okayMax int
	badMax  int

	pairwiseTarget  float64
	thresholdTarget float64
	recallTarget    float64

	asJSON  bool
	verbose bool
}

func main() {
	var o options
	flag.StringVar(&o.corpus, "corpus", "eval/corpus", "path to the corpus directory")
	flag.StringVar(&o.mode, "mode", "replay", "replay | record | live")
	flag.StringVar(&o.caseID, "case", "", "only evaluate this case ID (default: all)")
	flag.StringVar(&o.fixture, "fixtures", "eval/fixtures", "fixture directory for replay/record")

	flag.StringVar(&o.providerKind, "provider", "copilot", "AI provider: copilot, github-models, anthropic, gemini, local, openai")
	flag.StringVar(&o.model, "model", "gpt-4o", "model name")
	flag.StringVar(&o.baseURL, "base-url", "", "base URL override (local/openai/gemini)")
	flag.StringVar(&o.apiKey, "api-key", "", "API key/token (falls back to provider-specific env var)")

	flag.Float64Var(&o.temperature, "temperature", 0, "sampling temperature")
	flag.IntVar(&o.maxTokens, "max-tokens", 1500, "max tokens for the model response")
	flag.IntVar(&o.maxRetries, "max-retries", 1, "retries when the model returns an unparseable response")

	flag.IntVar(&o.goodMin, "good-min", eval.DefaultThresholds.GoodMin, "good messages should score at least this")
	flag.IntVar(&o.okayMin, "okay-min", eval.DefaultThresholds.OkayMin, "okay messages should score at least this")
	flag.IntVar(&o.okayMax, "okay-max", eval.DefaultThresholds.OkayMax, "okay messages should score at most this")
	flag.IntVar(&o.badMax, "bad-max", eval.DefaultThresholds.BadMax, "bad messages should score at most this")

	flag.Float64Var(&o.pairwiseTarget, "target-pairwise", eval.DefaultTargets.PairwiseAccuracy, "minimum pairwise accuracy to pass")
	flag.Float64Var(&o.thresholdTarget, "target-threshold", eval.DefaultTargets.ThresholdPass, "minimum threshold pass rate to pass")
	flag.Float64Var(&o.recallTarget, "target-recall", eval.DefaultTargets.DefectRecall, "minimum defect recall to pass")

	flag.BoolVar(&o.asJSON, "json", false, "emit the report as JSON")
	flag.BoolVar(&o.verbose, "v", false, "print each message as it is reviewed")
	flag.Parse()

	code, err := run(o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(o options) (int, error) {
	ctx := context.Background()

	corpus, err := eval.LoadCorpus(o.corpus)
	if err != nil {
		return 0, err
	}
	if o.caseID != "" {
		corpus, err = filterCase(corpus, o.caseID)
		if err != nil {
			return 0, err
		}
	}

	p, err := buildProvider(o)
	if err != nil {
		return 0, err
	}

	reviewer := review.NewReviewer(p,
		review.WithTemperature(o.temperature),
		review.WithMaxTokens(o.maxTokens),
		review.WithMaxRetries(o.maxRetries),
	)

	var onMsg eval.CaseProgress
	if o.verbose {
		onMsg = func(id string, m eval.Message) {
			fmt.Fprintf(os.Stderr, "reviewing %s [%s] %q\n", id, m.Quality, m.Text)
		}
	}

	th := eval.Thresholds{GoodMin: o.goodMin, OkayMin: o.okayMin, OkayMax: o.okayMax, BadMax: o.badMax}
	report, err := eval.NewHarness(reviewer).Evaluate(ctx, corpus, th, onMsg)
	if err != nil {
		return 0, err
	}

	targets := eval.Targets{
		PairwiseAccuracy: o.pairwiseTarget,
		ThresholdPass:    o.thresholdTarget,
		DefectRecall:     o.recallTarget,
	}

	if o.asJSON {
		if err := emitJSON(report, targets); err != nil {
			return 0, err
		}
	} else {
		printReport(report, targets, th)
	}

	if !report.Passes(targets) {
		return 1, nil
	}
	return 0, nil
}

// buildProvider constructs the provider for the chosen mode. In replay mode it
// returns a fixture-backed Replayer; in record mode it wraps the live provider
// with a Recorder; in live mode it returns the live provider directly.
func buildProvider(o options) (provider.Provider, error) {
	switch o.mode {
	case "replay":
		return provider.NewReplayer(o.fixture, o.providerKind, o.model), nil
	case "record", "live":
		p, err := provider.Build(provider.Config{
			Provider: o.providerKind,
			Model:    o.model,
			BaseURL:  o.baseURL,
			APIKey:   o.apiKey,
		})
		if err != nil {
			return nil, err
		}
		if o.mode == "record" {
			return provider.NewRecorder(p, o.fixture), nil
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unknown mode %q (want replay|record|live)", o.mode)
	}
}

func filterCase(c *eval.Corpus, id string) (*eval.Corpus, error) {
	for _, cs := range c.Cases {
		if cs.ID == id {
			return &eval.Corpus{Cases: []eval.Case{cs}}, nil
		}
	}
	return nil, fmt.Errorf("case %q not found in corpus", id)
}

func emitJSON(r eval.Report, t eval.Targets) error {
	out := map[string]any{
		"pairwise_accuracy": r.Pairwise.Accuracy(),
		"threshold_pass":    r.Threshold.PassRate(),
		"defect_recall":     r.Label.Recall(),
		"passes":            r.Passes(t),
		"pairwise":          r.Pairwise,
		"threshold":         r.Threshold,
		"label":             r.Label,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func printReport(r eval.Report, t eval.Targets, th eval.Thresholds) {
	fmt.Printf("Corpus results (%d messages)\n\n", len(r.Results))

	fmt.Printf("Pairwise discrimination (good > bad within a case)\n")
	fmt.Printf("  accuracy %s  (%d correct, %d ties, %d wrong of %d pairs)\n",
		pct(r.Pairwise.Accuracy(), t.PairwiseAccuracy),
		r.Pairwise.Correct, r.Pairwise.Ties, r.Pairwise.Wrong, r.Pairwise.Total())

	fmt.Printf("\nAbsolute thresholds (good >= %d, okay %d-%d, bad <= %d)\n", th.GoodMin, th.OkayMin, th.OkayMax, th.BadMax)
	fmt.Printf("  pass rate %s  (good %d/%d, okay %d/%d, bad %d/%d)\n",
		pct(r.Threshold.PassRate(), t.ThresholdPass),
		r.Threshold.GoodPass, r.Threshold.GoodTotal,
		r.Threshold.OkayPass, r.Threshold.OkayTotal,
		r.Threshold.BadPass, r.Threshold.BadTotal)

	fmt.Printf("\nDefect recall (tagged defects the reviewer flagged)\n")
	fmt.Printf("  recall %s  (%d caught, %d missed)\n",
		pct(r.Label.Recall(), t.DefectRecall),
		r.Label.TruePositives, r.Label.FalseNegatives)
	for _, cat := range r.Label.SortedCategories() {
		rec := r.Label.PerCategory[cat]
		fmt.Printf("    %-13s %d/%d\n", cat, rec.Caught, rec.Total)
	}

	fmt.Printf("\nGate: %s\n", passFail(r.Passes(t)))
}

// pct formats a fraction as a percentage with a pass/fail marker against target.
func pct(v, target float64) string {
	mark := "PASS"
	if v < target {
		mark = "FAIL"
	}
	return fmt.Sprintf("%5.1f%% [%s, target %.0f%%]", v*100, mark, target*100)
}

func passFail(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}
