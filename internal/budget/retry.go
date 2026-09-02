// Copyright 2026 DeMarco
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package budget

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	MaxAPIAttempts = 3
	MaxRetryAfter  = 30 * time.Second
)

// RetryDelay returns the wait before the next attempt.
// Prefer Retry-After-Ms, then Retry-After seconds, then 1s/2s backoff.
func RetryDelay(attempt int, raw *http.Response) time.Duration {
	if raw != nil {
		if ms := strings.TrimSpace(raw.Header.Get("Retry-After-Ms")); ms != "" {
			if millis, err := strconv.Atoi(ms); err == nil && millis >= 0 {
				delay := time.Duration(millis) * time.Millisecond
				if delay > MaxRetryAfter {
					return MaxRetryAfter
				}
				return delay
			}
		}
		if value := strings.TrimSpace(raw.Header.Get("Retry-After")); value != "" {
			if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
				if seconds > int(MaxRetryAfter/time.Second) {
					seconds = int(MaxRetryAfter / time.Second)
				}
				return time.Duration(seconds) * time.Second
			}
		}
	}
	if attempt <= 1 {
		return time.Second
	}
	return 2 * time.Second
}

// Retryable reports whether err should be retried for a live parent ctx.
// Callers should handle provider-specific SDK status errors first, then call
// this for shared classification (HTTP status on raw, EOF, timeouts, overloaded).
func Retryable(err error, ctx context.Context, raw *http.Response) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
		return false
	}
	if raw != nil {
		if raw.StatusCode == http.StatusTooManyRequests || raw.StatusCode >= 500 {
			return true
		}
		if raw.StatusCode >= 400 && raw.StatusCode < 500 {
			return false
		}
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connection reset") ||
		strings.Contains(message, "eof") ||
		strings.Contains(message, "timeout") ||
		strings.Contains(message, "overloaded")
}

// SleepContext waits for delay or returns when ctx is done.
func SleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
