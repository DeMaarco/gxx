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

package agent

import "context"

// Request identifies one provider attempt, including retries and summaries.
// It deliberately contains no prompts, request bodies, or credentials.
type Request struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Kind     string `json:"kind"`
	Attempt  int    `json:"attempt"`
}

// RequestObserver can enforce a budget before any model request is sent.
// After receives all usage the provider reported, including partial failures.
type RequestObserver interface {
	Before(context.Context, Request) error
	After(Request, Usage, error)
}

type requestObserverKey struct{}

func WithRequestObserver(ctx context.Context, observer RequestObserver) context.Context {
	return context.WithValue(ctx, requestObserverKey{}, observer)
}

func BeginRequest(ctx context.Context, request Request) (func(Usage, error), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	observer, _ := ctx.Value(requestObserverKey{}).(RequestObserver)
	if observer == nil {
		return func(Usage, error) {}, nil
	}
	if err := observer.Before(ctx, request); err != nil {
		return nil, err
	}
	return func(usage Usage, err error) { observer.After(request, usage, err) }, nil
}
