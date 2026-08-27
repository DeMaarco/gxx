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

package tools

import (
	"context"
	"strings"
	"time"

	"gxx/internal/agent"
)

const progressInterval = 70 * time.Millisecond

type ctxKey int

const (
	ctxEmitKey ctxKey = iota
	ctxCallKey
	ctxProgressKey
)

type progressState struct {
	last time.Time
}

func withToolContext(ctx context.Context, emit agent.EmitFunc, call agent.ToolCall) context.Context {
	ctx = context.WithValue(ctx, ctxEmitKey, emit)
	ctx = context.WithValue(ctx, ctxCallKey, call)
	ctx = context.WithValue(ctx, ctxProgressKey, &progressState{})
	return ctx
}

func reportProgress(ctx context.Context, detail string) {
	emitProgress(ctx, detail, false)
}

func reportProgressNow(ctx context.Context, detail string) {
	emitProgress(ctx, detail, true)
}

func emitProgress(ctx context.Context, detail string, force bool) {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return
	}
	emit, _ := ctx.Value(ctxEmitKey).(agent.EmitFunc)
	if emit == nil {
		return
	}
	state, _ := ctx.Value(ctxProgressKey).(*progressState)
	now := time.Now()
	if !force && state != nil && !state.last.IsZero() && now.Sub(state.last) < progressInterval {
		return
	}
	if state != nil {
		state.last = now
	}
	call, _ := ctx.Value(ctxCallKey).(agent.ToolCall)
	var toolCall *agent.ToolCall
	if call.ID != "" || call.Name != "" {
		toolCall = &agent.ToolCall{ID: call.ID, Name: call.Name}
	}
	agent.Emit(emit, agent.Event{
		Kind:     agent.EventToolProgress,
		ToolCall: toolCall,
		Text:     detail,
	})
}
