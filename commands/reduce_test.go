package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// fakeAnthropicClient is a scripted AnthropicClient test double: each call
// consumes the next entry, so a test can assert exactly what prompts were
// sent and control what each cue's decision is.
type fakeAnthropicClient struct {
	results []LineShortenResult
	errs    []error
	prompts []string
	calls   int
}

func (f *fakeAnthropicClient) ShortenLine(ctx context.Context, prompt string) (LineShortenResult, error) {
	f.prompts = append(f.prompts, prompt)
	i := f.calls
	f.calls++
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	if err != nil {
		return LineShortenResult{}, err
	}
	return f.results[i], nil
}

func TestReduceTargetWords(t *testing.T) {
	t.Run("computes target from measured speaking rate, 90% of budget", func(t *testing.T) {
		audioMs := 4000 // 4000ms for 10 words -> 400ms/word
		got := reduceTargetWords(2000, &audioMs, 10)
		// budget*0.9/ms_per_word = 2000*0.9/400 = 4.5 -> int() truncates to 4
		if got != 4 {
			t.Errorf("target = %d, want 4", got)
		}
	})

	t.Run("falls back to the current word count with no measured rate", func(t *testing.T) {
		if got := reduceTargetWords(2000, nil, 7); got != 7 {
			t.Errorf("target = %d, want 7 (no audio_ms yet)", got)
		}
		zero := 0
		if got := reduceTargetWords(2000, &zero, 7); got != 7 {
			t.Errorf("target = %d, want 7 (audio_ms present but zero words guarded separately)", got)
		}
	})

	t.Run("never targets below 1 word", func(t *testing.T) {
		audioMs := 100000
		got := reduceTargetWords(1, &audioMs, 50)
		if got != 1 {
			t.Errorf("target = %d, want 1 (clamped)", got)
		}
	})
}

func TestReduceText(t *testing.T) {
	t.Run("only basketed cues are sent to Claude", func(t *testing.T) {
		cues := []*SessionCue{
			{Index: 1, StartMs: 0, EndMs: 2000, Voice: "hana", CurrentText: "fine as is", NeedsHuman: false},
			{Index: 2, StartMs: 2000, EndMs: 4000, Voice: "hana", CurrentText: "this one is basketed", NeedsHuman: true},
		}
		client := &fakeAnthropicClient{results: []LineShortenResult{
			{Action: "shorten", Text: "shortened", Reason: ""},
		}}

		ReduceText(cues, client)

		if client.calls != 1 {
			t.Fatalf("calls = %d, want 1 (only the basketed cue)", client.calls)
		}
	})

	t.Run("a confident shortening applies like a manual edit and clears the basket", func(t *testing.T) {
		cues := []*SessionCue{
			{Index: 1, StartMs: 0, EndMs: 2000, Voice: "hana", CurrentText: "a rather long winded line", NeedsHuman: true, HumanReason: "too long", AiSuggestion: "stale", AiNote: "stale"},
		}
		client := &fakeAnthropicClient{results: []LineShortenResult{
			{Action: "shorten", Text: " a shorter line ", Reason: ""},
		}}

		result := ReduceText(cues, client)

		if len(result.Shortened) != 1 || result.Shortened[0].Index != 1 {
			t.Fatalf("shortened = %+v, want cue 1 recorded", result.Shortened)
		}
		if result.Shortened[0].ToText != "a shorter line" {
			t.Errorf("to_text = %q, want trimmed", result.Shortened[0].ToText)
		}
		c := cues[0]
		if c.CurrentText != "a shorter line" {
			t.Errorf("current_text = %q, want the trimmed shortened text", c.CurrentText)
		}
		if c.NeedsHuman || c.HumanReason != "" || c.AiSuggestion != "" || c.AiNote != "" {
			t.Errorf("cue = %+v, want basket/AI fields cleared", c)
		}
	})

	t.Run("a decline stays basketed with the AI's attempt and reasoning attached", func(t *testing.T) {
		cues := []*SessionCue{
			{Index: 1, StartMs: 0, EndMs: 2000, Voice: "hana", CurrentText: "a technical line", NeedsHuman: true},
		}
		client := &fakeAnthropicClient{results: []LineShortenResult{
			{Action: "decline", Text: "attempted draft", Reason: "would lose technical accuracy"},
		}}

		result := ReduceText(cues, client)

		if len(result.Declined) != 1 || result.Declined[0].Reason != "would lose technical accuracy" {
			t.Fatalf("declined = %+v", result.Declined)
		}
		c := cues[0]
		if !c.NeedsHuman {
			t.Errorf("cue should remain basketed on decline")
		}
		if c.AiSuggestion != "attempted draft" || c.AiNote != "would lose technical accuracy" {
			t.Errorf("cue = %+v, want AI suggestion/note attached", c)
		}
	})

	t.Run("a skipped cue is never sent to Claude even if it's still basketed", func(t *testing.T) {
		cues := []*SessionCue{
			{Index: 1, StartMs: 0, EndMs: 2000, Voice: "hana", CurrentText: "accepted as-is", NeedsHuman: true, Skipped: true},
		}
		client := &fakeAnthropicClient{results: []LineShortenResult{{Action: "shorten", Text: "should never be used"}}}

		result := ReduceText(cues, client)

		if client.calls != 0 {
			t.Fatalf("calls = %d, want 0 (skipped cue must never reach Claude)", client.calls)
		}
		if len(result.Shortened) != 0 || len(result.Declined) != 0 {
			t.Fatalf("result = %+v, want empty", result)
		}
	})

	t.Run("a failed AI call is recorded as declined, not fatal to the batch", func(t *testing.T) {
		cues := []*SessionCue{
			{Index: 1, StartMs: 0, EndMs: 2000, Voice: "hana", CurrentText: "one", NeedsHuman: true},
			{Index: 2, StartMs: 2000, EndMs: 4000, Voice: "hana", CurrentText: "two", NeedsHuman: true},
		}
		client := &fakeAnthropicClient{
			errs:    []error{errBoom, nil},
			results: []LineShortenResult{{}, {Action: "shorten", Text: "cut"}},
		}

		result := ReduceText(cues, client)

		if len(result.Declined) != 1 || result.Declined[0].Index != 1 {
			t.Fatalf("declined = %+v, want cue 1 recorded as a failed call", result.Declined)
		}
		if len(result.Shortened) != 1 || result.Shortened[0].Index != 2 {
			t.Fatalf("shortened = %+v, want cue 2 to still be processed", result.Shortened)
		}
	})

	t.Run("prompt names the adjacent lines by voice and text, or says none exist at the edges", func(t *testing.T) {
		cues := []*SessionCue{
			{Index: 1, StartMs: 0, EndMs: 2000, Voice: "matko", CurrentText: "first line", NeedsHuman: true},
		}
		client := &fakeAnthropicClient{results: []LineShortenResult{{Action: "decline", Text: "", Reason: "n/a"}}}

		ReduceText(cues, client)

		prompt := client.prompts[0]
		if !contains(prompt, "(none, this is the first line)") || !contains(prompt, "(none, this is the last line)") {
			t.Errorf("prompt = %q, want both edge placeholders", prompt)
		}
	})
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

var errBoom = errFor("network exploded")

type errFor string

func (e errFor) Error() string { return string(e) }

func TestAnthropicHTTPClient_ShortenLine(t *testing.T) {
	t.Run("sends a forced tool call and parses its structured input back", func(t *testing.T) {
		var gotAuth, gotVersion string
		var gotBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("x-api-key")
			gotVersion = r.Header.Get("anthropic-version")
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("content-type", "application/json")
			w.Write([]byte(`{"content":[{"type":"tool_use","input":{"action":"shorten","text":"cut","reason":"fits now"}}]}`))
		}))
		defer server.Close()

		client := newAnthropicClient("test-key", "claude-haiku-4-5")
		client.baseURL = server.URL

		got, err := client.ShortenLine(context.Background(), "shorten this")
		if err != nil {
			t.Fatalf("ShortenLine error: %v", err)
		}
		if got != (LineShortenResult{Action: "shorten", Text: "cut", Reason: "fits now"}) {
			t.Errorf("result = %+v", got)
		}
		if gotAuth != "test-key" {
			t.Errorf("x-api-key = %q, want test-key", gotAuth)
		}
		if gotVersion == "" {
			t.Errorf("anthropic-version header missing")
		}
		if gotBody["tool_choice"] == nil {
			t.Errorf("body = %+v, want a forced tool_choice so the response is always structured", gotBody)
		}
	})

	t.Run("a non-200 response is a clear error, not a silent empty result", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"message":"invalid x-api-key"}}`))
		}))
		defer server.Close()

		client := newAnthropicClient("bad-key", "")
		client.baseURL = server.URL

		if _, err := client.ShortenLine(context.Background(), "prompt"); err == nil {
			t.Fatalf("want an error on a 401 response")
		}
	})

	t.Run("live: the real API accepts this request shape", func(t *testing.T) {
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			t.Skip("ANTHROPIC_API_KEY not set; skipping live Anthropic call")
		}
		client := newAnthropicClient(apiKey, "")
		got, err := client.ShortenLine(context.Background(),
			"This line is spoken by hana in a Croatian-to-English dubbed dental-education video. It currently runs too long for its allotted audio time.\n\n"+
				"Previous line (n/a): (none, this is the first line)\nTHIS LINE (hana): Please remember to brush your teeth twice a day and floss regularly to avoid gum disease and cavities.\nNext line (n/a): (none, this is the last line)\n\n"+
				"Current length: 18 words. Target: about 8 words.\n\n"+
				"Shorten THIS LINE ONLY, as close to the target word count as you reasonably can. Preserve its meaning and technical/dental accuracy, keep it reading naturally as spoken dialogue, and preserve its grammatical connection to the neighboring lines if it is a sentence fragment (starts or ends mid-sentence rather than as a complete sentence). If you cannot shorten it that much without losing meaning, technical accuracy, or making it read unnaturally, decline instead of forcing a bad cut -- do not invent content or drop important qualifiers just to hit the target.")
		if err != nil {
			t.Fatalf("live ShortenLine error: %v", err)
		}
		if got.Action != "shorten" && got.Action != "decline" {
			t.Errorf("live result action = %q, want shorten or decline", got.Action)
		}
		if got.Reason == "" && got.Action == "decline" {
			t.Errorf("live decline had no reason: %+v", got)
		}
		t.Logf("live result: %+v", got)
	})
}
