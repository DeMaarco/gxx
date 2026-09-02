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

package budget_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"gxx/internal/budget"
)

func TestRetryableClassifiesSharedErrors(t *testing.T) {
	ctx := context.Background()
	raw429 := &http.Response{StatusCode: http.StatusTooManyRequests}
	if !budget.Retryable(errors.New("rate"), ctx, raw429) {
		t.Fatal("429 should be retryable")
	}
	raw400 := &http.Response{StatusCode: http.StatusBadRequest}
	if budget.Retryable(errors.New("bad"), ctx, raw400) {
		t.Fatal("400 should not be retryable")
	}
	if !budget.Retryable(io.ErrUnexpectedEOF, ctx, nil) {
		t.Fatal("EOF should be retryable")
	}
	if !budget.Retryable(errors.New("overloaded"), ctx, nil) {
		t.Fatal("overloaded should be retryable")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if budget.Retryable(errors.New("timeout"), canceled, nil) {
		t.Fatal("canceled parent should not retry")
	}
	if !budget.Retryable(context.DeadlineExceeded, ctx, nil) {
		t.Fatal("attempt deadline with live parent should retry")
	}
}

func TestRetryDelay(t *testing.T) {
	raw := &http.Response{Header: make(http.Header)}
	raw.Header.Set("Retry-After", "86400")
	if got := budget.RetryDelay(1, raw); got != budget.MaxRetryAfter {
		t.Fatalf("cap = %s", got)
	}
	raw.Header.Del("Retry-After")
	raw.Header.Set("Retry-After-Ms", "500")
	if got := budget.RetryDelay(1, raw); got != 500*time.Millisecond {
		t.Fatalf("ms = %s", got)
	}
	if got := budget.RetryDelay(1, nil); got != time.Second {
		t.Fatalf("default = %s", got)
	}
}
