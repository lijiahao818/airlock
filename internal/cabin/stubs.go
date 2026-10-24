package cabin

import (
	"context"
	"errors"

	"github.com/lijiahao818/airlock/internal/audit"
	"github.com/lijiahao818/airlock/internal/sandbox"
)

var errNotImplemented = errors.New("cabin: 未实现")

func (m *Manager) Start(ctx context.Context, id string) error { return errNotImplemented }

func (m *Manager) Stop(ctx context.Context, id string) error { return errNotImplemented }

func (m *Manager) AgentURL(id string) (string, error) { return "", errNotImplemented }

func (m *Manager) OpenTerminal(id string) error { return errNotImplemented }

func (m *Manager) Attach(ctx context.Context, id, token string) (sandbox.Session, error) {
	return nil, errNotImplemented
}

func (m *Manager) EntryFindings(id string) ([]Finding, error) { return nil, errNotImplemented }

func (m *Manager) ResetDecision(id, host string) error { return errNotImplemented }

func (m *Manager) Decide(holdID string, allow bool) error { return errNotImplemented }

func (m *Manager) Reset(ctx context.Context, id string) error { return errNotImplemented }

func (m *Manager) ReviewItems(id string) ([]ReviewItem, error) { return nil, errNotImplemented }

func (m *Manager) Revert(id, path string) error { return errNotImplemented }

func (m *Manager) Apply(id, path string) error { return errNotImplemented }

func (m *Manager) FinishReview(id string) error { return errNotImplemented }

func (m *Manager) Report(id string) (audit.Report, error) {
	return audit.Report{}, errNotImplemented
}
