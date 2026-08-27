package approval

import (
	"context"
	"sync"

	"gxx/internal/config"
)

// Policy auto-approves actions allowed by the current permission mode and
// otherwise defers to an inner approver.
type Policy struct {
	mu    sync.Mutex
	mode  string
	inner Approver
}

func NewPolicy(mode string, inner Approver) *Policy {
	canonical, err := config.CanonicalPermission(mode)
	if err != nil {
		canonical = config.PermissionAsk
	}
	return &Policy{mode: canonical, inner: inner}
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
	p.mu.Unlock()
	return nil
}

func (p *Policy) Approve(ctx context.Context, action Action) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	p.mu.Lock()
	mode := p.mode
	inner := p.inner
	p.mu.Unlock()

	if autoApproved(mode, action.Kind) {
		return true, nil
	}
	if inner == nil {
		return false, nil
	}
	return inner.Approve(ctx, action)
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
