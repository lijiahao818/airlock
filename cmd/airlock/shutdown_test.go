package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStopper func(ctx context.Context) error

func (f fakeStopper) StopAll(ctx context.Context) error { return f(ctx) }

func TestStopCabins(t *testing.T) {
	if err := stopCabins(fakeStopper(func(context.Context) error { return nil }), time.Second); err != nil {
		t.Fatalf("正常停止 = %v", err)
	}
	// StopAll 不理会 ctx 时，stopCabins 也在超时后返回。
	block := make(chan struct{})
	defer close(block)
	start := time.Now()
	err := stopCabins(fakeStopper(func(context.Context) error { <-block; return nil }), 50*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("超时 = %v，用了 %v", err, time.Since(start))
	}
}
