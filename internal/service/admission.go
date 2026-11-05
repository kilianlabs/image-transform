package service

import (
	"context"

	"github.com/kilianlabs/image-transform/internal/imageproc"
)

// AdmissionLimits counts active requests through response delivery and cleanup.
// Unspecified operation limits inherit Concurrent. Callers own waiting and retries.
type AdmissionLimits struct {
	Concurrent, InspectConcurrent, TransformConcurrent int
}

type operation int

const (
	inspect operation = iota
	transform
)

func (s *Service) acquire(ctx context.Context, kind operation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return imageproc.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	limit := s.limits.InspectConcurrent
	if kind == transform {
		limit = s.limits.TransformConcurrent
	}
	if s.active[inspect]+s.active[transform] >= s.limits.Concurrent || s.active[kind] >= limit {
		return imageproc.ErrBusy
	}
	s.active[kind]++
	s.wg.Add(1)
	return nil
}

func (s *Service) release(kind operation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active[kind]--
	s.wg.Done()
}
