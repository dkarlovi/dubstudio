package commands

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/haguro/elevenlabs-go"
)

// retryPolicy controls how a transient ElevenLabs failure is retried.
// attempts counts the first call, so attempts=1 means no retry at all.
type retryPolicy struct {
	attempts int
	base     time.Duration
}

// defaultTTSRetryPolicy is what generateMissingVoiceLines uses. Three
// attempts with 2s/4s of backoff: enough to ride out a rate limit or a
// brief 5xx without leaving a long-running Generate hanging for minutes,
// and cheap when it doesn't help. Note the client's own per-request
// timeout is 30s (see elevenLabsClient), so a fully stalled cue is
// bounded at roughly 30s*3 plus 6s of backoff.
func defaultTTSRetryPolicy() retryPolicy {
	return retryPolicy{attempts: 3, base: 2 * time.Second}
}

// retryableTTSError reports whether err is worth another attempt.
//
// This keys off the error *type* because that is the only signal the
// vendored client gives us: it builds *elevenlabs.APIError for 400/401 and
// *elevenlabs.ValidationError for 422, and returns every other non-200 --
// 429, 5xx, an unexpected 404 -- as a plain error with the response body
// already discarded, plus plain errors for dial/timeout failures. So the
// typed errors are exactly the deterministic request problems (a bad API
// key, a voice ID that doesn't exist, text the API rejects); retrying
// those spends time and credits to get the same answer back. Everything
// else is treated as transient.
//
// The flip side, worth knowing when reading a log: a genuinely permanent
// failure that arrives untyped (an unexpected 404, say) gets retried
// pointlessly before surfacing. There is no Retry-After to honor either,
// since the client drops response headers on the error path -- hence the
// blind exponential backoff in withRetry.
func retryableTTSError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *elevenlabs.APIError
	var valErr *elevenlabs.ValidationError
	return !errors.As(err, &apiErr) && !errors.As(err, &valErr)
}

// withRetry runs call until it succeeds, fails with something not worth
// retrying, or runs out of attempts, doubling the delay each time.
//
// retryable and sleep are parameters so this control flow is unit-testable
// without a real client or real delays -- the same pattern as
// runConvergenceLoop in service.go. Production callers pass
// retryableTTSError and time.Sleep.
func withRetry(p retryPolicy, retryable func(error) bool, sleep func(time.Duration), call func() error) error {
	if p.attempts < 1 {
		return fmt.Errorf("retryPolicy.attempts = %d, want at least 1", p.attempts)
	}

	var err error
	delay := p.base
	for attempt := 1; attempt <= p.attempts; attempt++ {
		err = call()
		if err == nil || !retryable(err) {
			return err
		}
		if attempt == p.attempts {
			break
		}
		log.Printf("ElevenLabs call failed (attempt %d/%d), retrying in %s: %v",
			attempt, p.attempts, delay, err)
		sleep(delay)
		delay *= 2
	}
	return fmt.Errorf("giving up after %d attempts: %w", p.attempts, err)
}
