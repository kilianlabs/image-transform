package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kilianlabs/image-transform/internal/imageproc"
)

type processor struct {
	inspect   func(context.Context, string) (imageproc.Metadata, error)
	transform func(context.Context, string, string, imageproc.Options) (imageproc.Metadata, error)
}

func (p processor) Inspect(ctx context.Context, path string) (imageproc.Metadata, error) {
	return p.inspect(ctx, path)
}
func (p processor) Transform(ctx context.Context, source, destination string, options imageproc.Options) (imageproc.Metadata, error) {
	if p.transform != nil {
		return p.transform(ctx, source, destination, options)
	}
	panic("unexpected transform")
}

func TestCancellationRetainsSlotAndFilesUntilNativeReturns(t *testing.T) {
	dir := t.TempDir()
	entered := make(chan string, 1)
	release := make(chan struct{})
	s := New(processor{inspect: func(ctx context.Context, path string) (imageproc.Metadata, error) {
		entered <- path
		<-release
		return imageproc.Metadata{}, nil
	}}, dir, 100, AdmissionLimits{Concurrent: 1}, time.Minute)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := s.Run(ctx, strings.NewReader("original"), nil); done <- err }()
	path := <-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("source removed during native execution", err)
	}
	if _, err := s.Run(t.Context(), strings.NewReader("new"), nil); !errors.Is(err, imageproc.ErrBusy) {
		t.Fatalf("slot released too soon: %v", err)
	}
	close(release)
	s.Stop()
	s.Wait()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("temporary files leaked")
	}
}

func TestResultOwnsSlotAndCleanup(t *testing.T) {
	dir := t.TempDir()
	s := New(processor{inspect: func(ctx context.Context, path string) (imageproc.Metadata, error) {
		return imageproc.Metadata{Width: 2}, nil
	}}, dir, 100, AdmissionLimits{Concurrent: 1}, time.Minute)
	result, err := s.Run(t.Context(), strings.NewReader("original"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Metadata.ByteCount != 8 || len(result.Metadata.SHA256) != 64 {
		t.Fatal(result.Metadata)
	}
	if _, err := s.Run(t.Context(), strings.NewReader("another"), nil); !errors.Is(err, imageproc.ErrBusy) {
		t.Fatal(err)
	}
	result.Close()
	result.Close()
	s.Stop()
	s.Wait()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal(entries)
	}
}

func TestLimitsAndErrors(t *testing.T) {
	for _, test := range []struct {
		name, body   string
		native, want error
	}{{"empty", "", nil, imageproc.ErrInvalid}, {"oversized", "12345", nil, imageproc.ErrTooLarge}, {"native failure", "123", imageproc.ErrFrames, imageproc.ErrFrames}} {
		t.Run(test.name, func(t *testing.T) {
			s := New(processor{inspect: func(context.Context, string) (imageproc.Metadata, error) { return imageproc.Metadata{}, test.native }}, t.TempDir(), 4, AdmissionLimits{Concurrent: 1}, time.Minute)
			_, err := s.Run(t.Context(), strings.NewReader(test.body), nil)
			if !errors.Is(err, test.want) {
				t.Fatalf("%v != %v", err, test.want)
			}
			s.Stop()
			s.Wait()
		})
	}
}

func TestDeadline(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	s := New(processor{inspect: func(context.Context, string) (imageproc.Metadata, error) {
		close(entered)
		<-release
		return imageproc.Metadata{}, nil
	}}, t.TempDir(), 100, AdmissionLimits{Concurrent: 1}, 50*time.Millisecond)
	done := make(chan error, 1)
	go func() { _, err := s.Run(t.Context(), strings.NewReader("source"), nil); done <- err }()
	<-entered
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(release)
	s.Stop()
	s.Wait()
}

func TestShutdownCancelsRequestsAndWaitsForNativeWork(t *testing.T) {
	dir := t.TempDir()
	entered := make(chan string, 1)
	release := make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	s := New(processor{inspect: func(ctx context.Context, path string) (imageproc.Metadata, error) {
		entered <- path
		<-ctx.Done()
		<-release
		return imageproc.Metadata{}, nil
	}}, dir, 100, AdmissionLimits{Concurrent: 1}, time.Minute)
	t.Cleanup(func() { finish(); s.Stop(); s.Wait() })
	done := make(chan error, 1)
	go func() { _, err := s.Run(t.Context(), strings.NewReader("source"), nil); done <- err }()
	path := <-entered
	s.Stop()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("active request was not cancelled: %v", err)
	}
	if _, err := s.Run(t.Context(), strings.NewReader("new"), nil); !errors.Is(err, imageproc.ErrUnavailable) {
		t.Fatalf("stopped service accepted work: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("source removed while native processing was active", err)
	}
	waited := make(chan struct{})
	go func() { s.Wait(); close(waited) }()
	select {
	case <-waited:
		t.Fatal("shutdown completed before native work stopped")
	case <-time.After(20 * time.Millisecond):
	}
	finish()
	<-waited
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("shutdown did not remove temporary files: %v %v", entries, err)
	}
}
