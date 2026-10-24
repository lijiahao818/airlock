package cabin

import (
	"context"
	"errors"

	"github.com/lijiahao818/airlock/internal/audit"
)

var errNotImplemented = errors.New("cabin: 未实现")

func (m *Manager) Reset(ctx context.Context, id string) error { return errNotImplemented }

func (m *Manager) ReviewItems(id string) ([]ReviewItem, error) { return nil, errNotImplemented }

func (m *Manager) Revert(id, path string) error { return errNotImplemented }

func (m *Manager) Apply(id, path string) error { return errNotImplemented }

func (m *Manager) FinishReview(id string) error { return errNotImplemented }

func (m *Manager) Report(id string) (audit.Report, error) {
	return audit.Report{}, errNotImplemented
}
