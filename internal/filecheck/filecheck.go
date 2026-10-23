package filecheck

import (
	"errors"

	"github.com/lijiahao818/airlock/internal/cabin"
	"github.com/lijiahao818/airlock/internal/staging"
)

var errNotImplemented = errors.New("filecheck: 未实现")

type Checker struct{}

var _ cabin.Inspector = Checker{}

// Entry 做进舱检查。
func (Checker) Entry(project string) ([]cabin.Finding, error) { return nil, errNotImplemented }

// Exit 为出舱审查标出高风险改动。
func (Checker) Exit(files string, changes []staging.Change) []cabin.Finding { return nil }
