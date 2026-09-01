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

package approval

import (
	"context"
	"sync"

	"gxx/internal/config"
)

// Policy auto-approves actions allowed by the current permission mode and
// otherwise defers to an inner approver. Ask and plan sessions never reach
// this policy: they only run read-only tools.
type Policy struct {
	mu         sync.Mutex
	mode       string
	inner      Approver
	remembered map[string]struct{}
}

func NewPolicy(mode string, inner Approver) *Policy {
	canonical, err := config.CanonicalPermission(mode)
	if err != nil {
		canonical = config.PermissionAsk
	}
	return &Policy{mode: canonical, inner: inner, remembered: make(map[string]struct{})}
}

func (p *Policy) Mode() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mode
}

func (p *Policy) SetMode(mode string) error {
	canonical, err := config.CanonicalPermission(mode)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.mode = canonical
	p.remembered = make(map[string]struct{})
	p.mu.Unlock()
	return nil
}

func (p *Policy) Approve(ctx context.Context, action Action) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}

	p.mu.Lock()
	if action.RepeatKey != "" {
		if _, ok := p.remembered[action.RepeatKey]; ok {
			p.mu.Unlock()
			return Decision{Approved: true}, nil
		}
	}
	mode := p.mode
	inner := p.inner
	p.mu.Unlock()

	if autoApproved(mode, action.Kind) {
		return Decision{Approved: true}, nil
	}
	if inner == nil {
		return Decision{}, nil
	}
	decision, err := inner.Approve(ctx, action)
	if err != nil {
		return decision, err
	}
	if decision.Remember && action.RepeatKey != "" {
		p.mu.Lock()
		p.remembered[action.RepeatKey] = struct{}{}
		p.mu.Unlock()
	}
	return decision, nil
}

func autoApproved(mode string, kind Kind) bool {
	canonical, err := config.CanonicalPermission(mode)
	if err != nil {
		return false
	}
	switch canonical {
	case config.PermissionAuto:
		return true
	case config.PermissionAutoWrites:
		return kind == KindWrite
	default:
		return false
	}
}
