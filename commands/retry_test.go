package commands

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/haguro/elevenlabs-go"
)

func TestRetryableTTSError(t *testing.T) {
	apiErr := &elevenlabs.APIError{Detail: elevenlabs.APIErrorDetail{
		Status:  "invalid_api_key",
		Message: "Invalid API key",
	}}
	valErr := &elevenlabs.ValidationError{}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil is not an error at all", nil, false},
		// 400/401: a bad key, a voice ID that doesn't exist, text the API
		// rejects. Deterministic -- a second attempt gets the same answer.
		{"APIError (400/401)", apiErr, false},
		{"wrapped APIError", fmt.Errorf("synthesizing cue #3: %w", apiErr), false},
		{"ValidationError (422)", valErr, false},
		{"wrapped ValidationError", fmt.Errorf("cue #1: %w", valErr), false},
		// Everything else reaches us untyped, body already discarded by the
		// client -- including the cases actually worth retrying.
		{"rate limit / 5xx, which the client returns untyped", errors.New(`unexpected HTTP status "429 Too Many Requests" returned from server`), true},
		{"server error, likewise untyped", errors.New(`unexpected HTTP status "503 Service Unavailable" returned from server`), true},
		{"transport failure", errors.New("dial tcp: i/o timeout"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := retryableTTSError(tc.err); got != tc.want {
				t.Errorf("retryableTTSError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestWithRetry(t *testing.T) {
	transient := errors.New("unexpected HTTP status \"503 Service Unavailable\" returned from server")
	permanent := &elevenlabs.APIError{Detail: elevenlabs.APIErrorDetail{Message: "Invalid API key"}}
	alwaysRetry := func(error) bool { return true }

	// recorder collects the backoff delays instead of sleeping, so these
	// tests run instantly and assert the exact schedule.
	newSleeper := func(slept *[]time.Duration) func(time.Duration) {
		return func(d time.Duration) { *slept = append(*slept, d) }
	}

	t.Run("a call that succeeds first time is not retried and never sleeps", func(t *testing.T) {
		var slept []time.Duration
		calls := 0
		err := withRetry(retryPolicy{attempts: 3, base: time.Second}, alwaysRetry, newSleeper(&slept), func() error {
			calls++
			return nil
		})
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if calls != 1 {
			t.Errorf("calls = %d, want 1", calls)
		}
		if len(slept) != 0 {
			t.Errorf("slept %v, want no sleeps", slept)
		}
	})

	t.Run("a transient failure is retried and the result of the retry is returned", func(t *testing.T) {
		var slept []time.Duration
		calls := 0
		err := withRetry(retryPolicy{attempts: 3, base: 2 * time.Second}, retryableTTSError, newSleeper(&slept), func() error {
			calls++
			if calls == 1 {
				return transient
			}
			return nil
		})
		if err != nil {
			t.Fatalf("err = %v, want nil after a successful retry", err)
		}
		if calls != 2 {
			t.Errorf("calls = %d, want 2", calls)
		}
		if len(slept) != 1 || slept[0] != 2*time.Second {
			t.Errorf("slept %v, want one 2s backoff", slept)
		}
	})

	t.Run("backoff doubles and the last error is wrapped once attempts run out", func(t *testing.T) {
		var slept []time.Duration
		calls := 0
		err := withRetry(retryPolicy{attempts: 3, base: 2 * time.Second}, retryableTTSError, newSleeper(&slept), func() error {
			calls++
			return transient
		})
		if err == nil {
			t.Fatal("err = nil, want the exhausted-retries failure")
		}
		if calls != 3 {
			t.Errorf("calls = %d, want 3 (the policy's full budget)", calls)
		}
		want := []time.Duration{2 * time.Second, 4 * time.Second}
		if len(slept) != len(want) || slept[0] != want[0] || slept[1] != want[1] {
			t.Errorf("slept %v, want %v", slept, want)
		}
		// The caller has to be able to see what actually failed.
		if !errors.Is(err, transient) {
			t.Errorf("err = %v, does not wrap the underlying failure", err)
		}
		if !strings.Contains(err.Error(), "giving up after 3 attempts") {
			t.Errorf("err = %q, want it to say how many attempts were made", err)
		}
	})

	t.Run("a deterministic failure is returned immediately, unwrapped", func(t *testing.T) {
		var slept []time.Duration
		calls := 0
		err := withRetry(retryPolicy{attempts: 5, base: time.Second}, retryableTTSError, newSleeper(&slept), func() error {
			calls++
			return permanent
		})
		if calls != 1 {
			t.Errorf("calls = %d, want 1 -- retrying a bad key spends time to get the same answer", calls)
		}
		if len(slept) != 0 {
			t.Errorf("slept %v, want no sleeps", slept)
		}
		if err != error(permanent) {
			t.Errorf("err = %v, want the original error returned as-is", err)
		}
		if strings.Contains(err.Error(), "giving up") {
			t.Errorf("err = %q, should not read as exhausted retries", err)
		}
	})

	t.Run("attempts=1 means try once, no retry", func(t *testing.T) {
		var slept []time.Duration
		calls := 0
		err := withRetry(retryPolicy{attempts: 1, base: time.Second}, alwaysRetry, newSleeper(&slept), func() error {
			calls++
			return transient
		})
		if calls != 1 {
			t.Errorf("calls = %d, want 1", calls)
		}
		if len(slept) != 0 {
			t.Errorf("slept %v, want no sleeps", slept)
		}
		if !errors.Is(err, transient) {
			t.Errorf("err = %v, want it to wrap the failure", err)
		}
	})

	t.Run("a policy with no attempts is a programming error, not a silent no-op", func(t *testing.T) {
		calls := 0
		err := withRetry(retryPolicy{attempts: 0}, alwaysRetry, func(time.Duration) {}, func() error {
			calls++
			return nil
		})
		if err == nil {
			t.Error("err = nil, want a complaint about the policy")
		}
		if calls != 0 {
			t.Errorf("calls = %d, want the call never made", calls)
		}
	})
}

func TestDefaultTTSRetryPolicy(t *testing.T) {
	p := defaultTTSRetryPolicy()
	if p.attempts < 2 {
		t.Errorf("attempts = %d, want more than one so a transient failure is actually retried", p.attempts)
	}
	if p.base <= 0 {
		t.Errorf("base = %s, want a positive backoff", p.base)
	}
	// Bound the worst case: the client's own per-request timeout is 30s,
	// so the backoff must not be what makes a stalled cue unbounded.
	total := time.Duration(0)
	d := p.base
	for i := 1; i < p.attempts; i++ {
		total += d
		d *= 2
	}
	if total > 30*time.Second {
		t.Errorf("total backoff = %s, too long to sit inside one Generate", total)
	}
}
