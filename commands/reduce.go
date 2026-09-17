package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Ported from dub-studio's core.py (reduce_text, _REDUCE_PROMPT,
// _LineShortenResult). Unlike every other ported feature, this one has no
// parity bridge test against the Python PoC: it's an LLM call, not
// deterministic business logic, so byte-identical parity across
// implementations isn't a meaningful bar. Go-only tests, driven through
// the AnthropicClient interface, are the whole test story here.

const defaultReduceModel = "claude-haiku-4-5"

const anthropicAPIBaseURL = "https://api.anthropic.com/v1/messages"

// reducePrompt mirrors dub-studio's _REDUCE_PROMPT verbatim.
const reducePrompt = `This line is spoken by %s in a Croatian-to-English dubbed dental-education video. It currently runs too long for its allotted audio time.

Previous line (%s): %s
THIS LINE (%s): %s
Next line (%s): %s

Current length: %d words. Target: about %d words.

Shorten THIS LINE ONLY, as close to the target word count as you reasonably can. Preserve its meaning and technical/dental accuracy, keep it reading naturally as spoken dialogue, and preserve its grammatical connection to the neighboring lines if it is a sentence fragment (starts or ends mid-sentence rather than as a complete sentence). If you cannot shorten it that much without losing meaning, technical accuracy, or making it read unnaturally, decline instead of forcing a bad cut -- do not invent content or drop important qualifiers just to hit the target.`

// LineShortenResult is Claude's structured decision for one line, mirroring
// dub-studio's _LineShortenResult (a Pydantic model there, a forced tool
// call here -- see anthropicHTTPClient.ShortenLine).
type LineShortenResult struct {
	Action string // "shorten" or "decline"
	Text   string
	Reason string
}

// AnthropicClient is the seam ReduceText calls through, so tests can script
// Claude's responses without spending real API credits.
type AnthropicClient interface {
	ShortenLine(ctx context.Context, prompt string) (LineShortenResult, error)
}

// ReduceEdit records one line Claude confidently shortened.
type ReduceEdit struct {
	Index     int    `json:"index"`
	FromWords int    `json:"from_words"`
	ToText    string `json:"to_text"`
}

// ReduceDecline records one line that stayed basketed, either because
// Claude declined outright or because the call itself failed.
type ReduceDecline struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

// ReduceResult mirrors dub-studio's reduce_text return shape.
type ReduceResult struct {
	Shortened []ReduceEdit    `json:"shortened"`
	Declined  []ReduceDecline `json:"declined"`
}

// reduceTargetWords mirrors dub-studio's target_words calculation: convert
// the cue's own measured speaking rate (audio_ms / word count from the last
// real generation) into a word budget for 90% of its real timing budget, so
// the shortened line has slack rather than landing exactly on the edge. With
// no measured rate yet (no generation, or a zero-word line), there's nothing
// to convert against, so the current word count itself is the target -- Claude
// still gets to decide whether that's already short enough.
func reduceTargetWords(budgetMs int, audioMs *int, words int) int {
	var msPerWord float64
	if audioMs != nil && words > 0 {
		msPerWord = float64(*audioMs) / float64(words)
	}
	if msPerWord <= 0 {
		return words
	}
	target := int(float64(budgetMs) * 0.9 / msPerWord)
	if target < 1 {
		target = 1
	}
	return target
}

func buildReducePrompt(voice, prevVoice, prevText, text, nextVoice, nextText string, words, targetWords int) string {
	return fmt.Sprintf(reducePrompt, voice, prevVoice, prevText, voice, text, nextVoice, nextText, words, targetWords)
}

// ReduceText calls Claude to draft a shortened version of every basketed
// cue, sized to its real timing budget (realBudgetsMs, the same budget
// auto-fix scheduling uses) via that cue's own measured speaking rate. A
// confident shortening applies immediately, same effect as a manual edit,
// including clearing the basket flag. A cue Claude declines to touch (or
// that fails outright) stays basketed; a decline keeps its attempted draft
// and reasoning attached (AiSuggestion / AiNote) so the human isn't
// starting from a blank line. Ported from dub-studio's core.reduce_text.
func ReduceText(cues []*SessionCue, client AnthropicClient) ReduceResult {
	budgets := realBudgetsMs(toAutoFixCues(cues))
	byIndex := make(map[int]*SessionCue, len(cues))
	for _, c := range cues {
		byIndex[c.Index] = c
	}

	result := ReduceResult{Shortened: []ReduceEdit{}, Declined: []ReduceDecline{}}
	for _, c := range cues {
		if !c.NeedsHuman || c.Skipped {
			continue
		}

		budget := c.WindowMs()
		if b, ok := budgets[c.Index]; ok {
			budget = b
		}
		words := len(strings.Fields(c.CurrentText))
		target := reduceTargetWords(budget, c.AudioMs, words)

		prevVoice, prevText := "n/a", "(none, this is the first line)"
		if prev, ok := byIndex[c.Index-1]; ok {
			prevVoice, prevText = prev.Voice, prev.CurrentText
		}
		nextVoice, nextText := "n/a", "(none, this is the last line)"
		if next, ok := byIndex[c.Index+1]; ok {
			nextVoice, nextText = next.Voice, next.CurrentText
		}

		prompt := buildReducePrompt(c.Voice, prevVoice, prevText, c.CurrentText, nextVoice, nextText, words, target)

		lr, err := client.ShortenLine(context.Background(), prompt)
		if err != nil {
			result.Declined = append(result.Declined, ReduceDecline{Index: c.Index, Reason: "AI call failed: " + err.Error()})
			continue
		}

		if lr.Action == "shorten" && strings.TrimSpace(lr.Text) != "" {
			text := strings.TrimSpace(lr.Text)
			c.CurrentText = text
			c.NeedsHuman = false
			c.HumanReason = ""
			c.AiSuggestion = ""
			c.AiNote = ""
			result.Shortened = append(result.Shortened, ReduceEdit{Index: c.Index, FromWords: words, ToText: text})
		} else {
			c.AiSuggestion = strings.TrimSpace(lr.Text)
			c.AiNote = lr.Reason
			result.Declined = append(result.Declined, ReduceDecline{Index: c.Index, Reason: lr.Reason})
		}
	}
	return result
}

// anthropicHTTPClient is the real AnthropicClient: a direct HTTP call to
// the Messages API (no SDK dependency, matching this codebase's existing
// preference for the standard library over a new dependency for a single
// call shape -- see the normalize-target-db rationale in CLAUDE.local.md).
// A tool call is forced (tool_choice) so the response is always the exact
// {action,text,reason} shape, the Go equivalent of the Python SDK's
// messages.parse(output_format=...).
type anthropicHTTPClient struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

func newAnthropicClient(apiKey, model string) *anthropicHTTPClient {
	if model == "" {
		model = defaultReduceModel
	}
	return &anthropicHTTPClient{
		apiKey:     apiKey,
		model:      model,
		baseURL:    anthropicAPIBaseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type anthropicShortenInput struct {
	Action string `json:"action"`
	Text   string `json:"text"`
	Reason string `json:"reason"`
}

func (a *anthropicHTTPClient) ShortenLine(ctx context.Context, prompt string) (LineShortenResult, error) {
	reqBody := map[string]any{
		"model":      a.model,
		"max_tokens": 512,
		"messages":   []map[string]any{{"role": "user", "content": prompt}},
		"tools": []map[string]any{{
			"name":        "shorten_line",
			"description": "Return the shortening decision for this line.",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"shorten", "decline"},
						"description": "\"shorten\" if you can produce a good shortened version that fits the target; \"decline\" if you cannot without losing meaning, technical accuracy, or unnatural phrasing.",
					},
					"text": map[string]any{
						"type":        "string",
						"description": "If action is \"shorten\": the actual shortened line, ready to use as spoken dialogue as-is -- not the original line, not commentary about it. If action is \"decline\": your best attempted shortened draft anyway (even though you're declining to apply it), so a human isn't starting from a blank line.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "A short explanation of your decision: why this shortened version works, or why you declined to shorten it that much.",
					},
				},
				"required": []string{"action", "text", "reason"},
			},
		}},
		"tool_choice": map[string]any{"type": "tool", "name": "shorten_line"},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return LineShortenResult{}, fmt.Errorf("encoding anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL, bytes.NewReader(body))
	if err != nil {
		return LineShortenResult{}, fmt.Errorf("building anthropic request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return LineShortenResult{}, fmt.Errorf("calling anthropic: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return LineShortenResult{}, fmt.Errorf("reading anthropic response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return LineShortenResult{}, fmt.Errorf("anthropic API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Content []struct {
			Type  string                `json:"type"`
			Input anthropicShortenInput `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return LineShortenResult{}, fmt.Errorf("decoding anthropic response: %w", err)
	}
	for _, block := range parsed.Content {
		if block.Type == "tool_use" {
			return LineShortenResult{Action: block.Input.Action, Text: block.Input.Text, Reason: block.Input.Reason}, nil
		}
	}
	return LineShortenResult{}, fmt.Errorf("anthropic response had no tool_use block")
}
