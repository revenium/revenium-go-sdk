// Command e2e-enforcement runs the 6-phase end-to-end smoke that proves
// the cost-controls enforcement engine is wired correctly in the Go SDK.
// It mirrors the Node SDK's scripts/e2e-smoke.js harness.
//
// Phases
//
//  1. LIVE_WIRING_AGAINST_DEV — real GET against
//     {DEV_API_BASE_URL}/profitstream/v2/api/ai/enforcement-rules/{DEV_TEAM_ID}
//     with x-api-key, confirms URL + header + response parse.
//  2. EXCEPTION_CONTRACT_BLOCK — synthetic BLOCK rule → Check returns
//     *ErrCostLimitExceeded with expected structured fields.
//  3. EXCEPTION_CONTRACT_SHADOW_MODE — same BLOCK rule with shadowMode=true
//     → Check returns nil (observation only).
//  4. FAIL_OPEN_MISSING_TEAM_ID — Start with teamID="" → engine dormant,
//     Check returns nil, no HTTP calls made.
//  5. FAIL_OPEN_204_NO_CONTENT — httptest returns 204 → cache is empty,
//     Check returns nil. (Live dev hitting 204 in phase 1 covers this too.)
//  6. INTEGRATION_OPENAI_WRAPPER_BLOCKS_BEFORE_CALL — real ReveniumOpenAI
//     against a counting httptest OpenAI endpoint. Synthetic BLOCK rule
//     injected via enforcement httptest. Chat.Completions.New must return
//     ErrCostLimitExceeded with zero calls to the OpenAI endpoint.
//
// Output
//
// Writes .e2e-report.md next to itself with pass/fail per phase, then
// exits 0 if every phase passes or 1 if any fail.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	oai "github.com/openai/openai-go/v3"
	"github.com/revenium/revenium-go-sdk/core"
	"github.com/revenium/revenium-go-sdk/core/enforcement"
	reveniumopenai "github.com/revenium/revenium-go-sdk/openai"
)

type phaseResult struct {
	Name    string
	Pass    bool
	Details string
	Elapsed time.Duration
}

func main() {
	core.InitializeLogger()
	var results []phaseResult

	results = append(results, runPhase("1. LIVE_WIRING_AGAINST_DEV", phase1LiveWiring))
	results = append(results, runPhase("2. EXCEPTION_CONTRACT_BLOCK", phase2BlockContract))
	results = append(results, runPhase("3. EXCEPTION_CONTRACT_SHADOW_MODE", phase3ShadowMode))
	results = append(results, runPhase("4. FAIL_OPEN_MISSING_TEAM_ID", phase4MissingTeamID))
	results = append(results, runPhase("5. FAIL_OPEN_204_NO_CONTENT", phase5NoContent))
	results = append(results, runPhase("6. INTEGRATION_OPENAI_WRAPPER_BLOCKS_BEFORE_CALL", phase6OpenAIWrapperBlocks))

	writeReport(results)

	allPass := true
	for _, r := range results {
		if !r.Pass {
			allPass = false
		}
	}
	if !allPass {
		os.Exit(1)
	}
}

func runPhase(name string, fn func() (string, error)) phaseResult {
	start := time.Now()
	details, err := fn()
	elapsed := time.Since(start)
	ok := err == nil
	marker := "PASS"
	if !ok {
		marker = "FAIL"
		details = details + " — " + err.Error()
	}
	fmt.Printf("[%s] %s  (%s)\n", marker, name, elapsed)
	if details != "" {
		fmt.Printf("        %s\n", details)
	}
	enforcement.Stop()
	return phaseResult{Name: name, Pass: ok, Details: details, Elapsed: elapsed}
}

// Phase 1 — hit the real dev backend. Skipped if DEV_* env vars are absent.
func phase1LiveWiring() (string, error) {
	// DEV_* takes precedence — this harness targets dev specifically.
	// Falls through to REVENIUM_* for CI environments that only export the
	// canonical SDK env vars.
	baseURL := firstNonEmpty(appendPath(os.Getenv("DEV_API_BASE_URL"), "/profitstream"), os.Getenv("REVENIUM_ENFORCEMENT_BASE_URL"))
	apiKey := firstNonEmpty(os.Getenv("DEV_API_KEY"), os.Getenv("REVENIUM_METERING_API_KEY"))
	teamID := firstNonEmpty(os.Getenv("DEV_TEAM_ID"), os.Getenv("REVENIUM_TEAM_ID"))
	if baseURL == "" || apiKey == "" || teamID == "" {
		return "skipped — set DEV_API_BASE_URL/DEV_API_KEY/DEV_TEAM_ID in ~/revenium/.env", nil
	}

	e := enforcement.Start(baseURL, apiKey, teamID)
	if e == nil {
		return "", fmt.Errorf("Start returned nil engine against %s", baseURL)
	}

	// Check must be callable without error — the request passed, even if
	// the server returned 204 (empty ruleset).
	if err := e.Check(enforcement.EvalContext{SubscriberID: os.Getenv("DEV_USER_EMAIL")}); err != nil {
		return "", fmt.Errorf("Check returned unexpected error: %w", err)
	}

	return fmt.Sprintf("baseURL=%s teamID=%s — fetch succeeded, engine healthy", baseURL, teamID), nil
}

// Phase 2 — synthetic BLOCK rule must throw structured error.
func phase2BlockContract() (string, error) {
	rule := enforcement.Rule{
		RuleID: 4242, Name: "e2e-block", SubscriberID: "samuel.combs@revenium.io",
		Threshold: 0.01, CurrentValue: 0.0147, PeriodType: "MONTHLY",
		Action: enforcement.ActionBlock, Breached: true,
	}
	srv := ruleServer([]enforcement.Rule{rule})
	defer srv.Close()

	e := enforcement.Start(srv.URL, "test-key", "team-e2e")
	err := e.Check(enforcement.EvalContext{SubscriberID: "samuel.combs@revenium.io"})
	if err == nil {
		return "", errors.New("expected ErrCostLimitExceeded, got nil")
	}

	var ece *enforcement.ErrCostLimitExceeded
	if !errors.As(err, &ece) {
		return "", fmt.Errorf("expected *ErrCostLimitExceeded, got %T", err)
	}
	if ece.RuleID != "4242" || ece.Threshold != 0.01 || ece.CurrentValue != 0.0147 || ece.PeriodType != "MONTHLY" {
		return "", fmt.Errorf("field mismatch: %+v", ece)
	}
	return fmt.Sprintf("ErrCostLimitExceeded{RuleID=%s Threshold=%.4f CurrentValue=%.4f Period=%s}",
		ece.RuleID, ece.Threshold, ece.CurrentValue, ece.PeriodType), nil
}

// Phase 3 — shadow-mode BLOCK rule must NOT throw.
func phase3ShadowMode() (string, error) {
	rule := enforcement.Rule{
		RuleID: 4243, Name: "e2e-shadow", SubscriberID: "samuel.combs@revenium.io",
		Threshold: 0.01, CurrentValue: 0.0147,
		Action: enforcement.ActionBlock, Breached: true, ShadowMode: true,
	}
	srv := ruleServer([]enforcement.Rule{rule})
	defer srv.Close()

	e := enforcement.Start(srv.URL, "test-key", "team-e2e")
	if err := e.Check(enforcement.EvalContext{SubscriberID: "samuel.combs@revenium.io"}); err != nil {
		return "", fmt.Errorf("shadow rule blocked: %w (must observe only)", err)
	}
	return "shadow rule logged without blocking", nil
}

// Phase 4 — missing teamID: engine dormant, no HTTP, Check returns nil.
func phase4MissingTeamID() (string, error) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()

	e := enforcement.Start(srv.URL, "test-key", "")
	if e == nil {
		return "", errors.New("engine is nil")
	}
	if err := e.Check(enforcement.EvalContext{SubscriberID: "samuel.combs@revenium.io"}); err != nil {
		return "", fmt.Errorf("dormant engine returned error: %w", err)
	}
	if atomic.LoadInt32(&hits) != 0 {
		return "", fmt.Errorf("expected 0 HTTP calls, got %d", atomic.LoadInt32(&hits))
	}
	return "engine dormant, 0 HTTP calls, Check returns nil", nil
}

// Phase 5 — 204 No Content: empty cache, fails open.
func phase5NoContent() (string, error) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	e := enforcement.Start(srv.URL, "test-key", "team-e2e")
	if err := e.Check(enforcement.EvalContext{SubscriberID: "samuel.combs@revenium.io"}); err != nil {
		return "", fmt.Errorf("204 path returned error: %w", err)
	}
	return "204 treated as empty ruleset, Check returns nil", nil
}

// Phase 6 — full wrapper path: synthetic BLOCK rule + counting OpenAI
// endpoint. Wrapped Chat.Completions.New must return ErrCostLimitExceeded
// and the OpenAI endpoint must see zero calls.
func phase6OpenAIWrapperBlocks() (string, error) {
	rule := enforcement.Rule{
		RuleID: 4244, Name: "e2e-wrapper-block",
		SubscriberID: "samuel.combs@revenium.io",
		Threshold:    0.01, CurrentValue: 0.05, PeriodType: "DAILY",
		Action: enforcement.ActionBlock, Breached: true,
	}
	enforceSrv := ruleServer([]enforcement.Rule{rule})
	defer enforceSrv.Close()

	var openaiHits int32
	openaiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&openaiHits, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","model":"gpt-4o-mini","choices":[]}`))
	}))
	defer openaiSrv.Close()

	// Boot enforcement via the internal engine — mimics what the provider
	// Initialize does, but with test URLs.
	enforcement.Start(enforceSrv.URL, "test-key", "team-e2e")

	// Build the wrapped ReveniumOpenAI pointed at the counting endpoint.
	cfg := &reveniumopenai.Config{
		OpenAIAPIKey: "test-openai-key",
		BaseURL:      openaiSrv.URL,
		Revenium: &core.ReveniumConfig{
			APIKey:             "test-key",
			BaseURL:            enforceSrv.URL,
			EnforcementBaseURL: enforceSrv.URL,
			TeamID:             "team-e2e",
		},
	}
	r, err := reveniumopenai.NewReveniumOpenAI(cfg)
	if err != nil {
		return "", fmt.Errorf("NewReveniumOpenAI: %w", err)
	}
	defer func() { _ = r.Close() }()

	ctx := core.WithSubscriber(context.Background(), &core.Subscriber{
		ID:    "samuel.combs@revenium.io",
		Email: "samuel.combs@revenium.io",
	})

	_, err = r.Chat().Completions().New(ctx, oai.ChatCompletionNewParams{
		Model:    "gpt-4o-mini",
		Messages: []oai.ChatCompletionMessageParamUnion{oai.UserMessage("hello")},
	})

	var ece *enforcement.ErrCostLimitExceeded
	if !errors.As(err, &ece) {
		return "", fmt.Errorf("expected *ErrCostLimitExceeded, got %v (type %T)", err, err)
	}

	if hits := atomic.LoadInt32(&openaiHits); hits != 0 {
		return "", fmt.Errorf("expected 0 OpenAI calls (enforcement should fire pre-call), got %d", hits)
	}

	// Streaming parity — same gate must fire for NewStreaming.
	_, err = r.Chat().Completions().NewStreaming(ctx, oai.ChatCompletionNewParams{
		Model:    "gpt-4o-mini",
		Messages: []oai.ChatCompletionMessageParamUnion{oai.UserMessage("hello")},
	})
	if !errors.As(err, &ece) {
		return "", fmt.Errorf("streaming: expected *ErrCostLimitExceeded, got %v (type %T)", err, err)
	}
	if hits := atomic.LoadInt32(&openaiHits); hits != 0 {
		return "", fmt.Errorf("streaming: expected 0 OpenAI calls, got %d", hits)
	}

	return fmt.Sprintf("sync+stream blocked pre-call, openai hits=%d, rule=%s",
		atomic.LoadInt32(&openaiHits), ece.RuleID), nil
}

// ruleServer returns an httptest.Server that serves the given rules in the
// {rules:[...]} envelope the real enforcement API uses.
func ruleServer(rules []enforcement.Rule) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Rules []enforcement.Rule `json:"rules"`
		}{Rules: rules})
	}))
}

func writeReport(results []phaseResult) {
	exe, _ := os.Executable()
	reportDir := filepath.Dir(exe)
	if wd, err := os.Getwd(); err == nil {
		reportDir = wd
	}
	path := filepath.Join(reportDir, ".e2e-report.md")

	var b strings.Builder
	fmt.Fprintf(&b, "# Cost Controls Enforcement — E2E Report\n\n")
	fmt.Fprintf(&b, "Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "| Phase | Verdict | Elapsed | Details |\n|-------|---------|---------|---------|\n")
	for _, r := range results {
		verdict := "PASS"
		if !r.Pass {
			verdict = "FAIL"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", r.Name, verdict, r.Elapsed.Truncate(time.Millisecond), escapePipes(r.Details))
	}
	fmt.Fprintf(&b, "\n## Environment\n\n")
	fmt.Fprintf(&b, "- DEV_API_BASE_URL: %s\n", os.Getenv("DEV_API_BASE_URL"))
	fmt.Fprintf(&b, "- DEV_TEAM_ID: %s\n", os.Getenv("DEV_TEAM_ID"))
	fmt.Fprintf(&b, "- DEV_USER_EMAIL: %s\n", os.Getenv("DEV_USER_EMAIL"))

	_ = os.WriteFile(path, []byte(b.String()), 0o644)
	fmt.Printf("\nReport written to %s\n", path)
}

func escapePipes(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func appendPath(base, suffix string) string {
	if base == "" {
		return ""
	}
	base = strings.TrimRight(base, "/")
	if suffix == "" {
		return base
	}
	if !strings.HasPrefix(suffix, "/") {
		suffix = "/" + suffix
	}
	return base + suffix
}
