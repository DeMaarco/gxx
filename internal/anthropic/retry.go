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

package anthropic

import (
	"context"
	"errors"
	"net/http"
	"time"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"

	"gxx/internal/budget"
)

const maxAPIAttempts = budget.MaxAPIAttempts

var maxRetryAfter = budget.MaxRetryAfter

func retryDelay(attempt int, raw *http.Response) time.Duration {
	return budget.RetryDelay(attempt, raw)
}

func retryable(err error, ctx context.Context, raw *http.Response) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	var apiErr *anthropicsdk.Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500
	}
	return budget.Retryable(err, ctx, raw)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	return budget.SleepContext(ctx, delay)
}
