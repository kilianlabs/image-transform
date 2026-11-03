// Package service owns admission, temporary files and cancellation lifetimes.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kilianlabs/image-transform/internal/imageproc"
)

type Result struct {
	Metadata imageproc.Metadata
	Path     string
	close    func()
}

// Close releases the request slot and temporary files. It is safe to call twice.
func (r *Result) Close() { r.close() }

type completion struct {
	result *Result
	err    error
}

type Service struct {
	processor imageproc.ImageProcessor
	tempDir   string
	maxInput  int64
	timeout   time.Duration
	slots     chan struct{}
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	stopped   bool
	wg        sync.WaitGroup
}

func New(p imageproc.ImageProcessor, tempDir string, maxInput int64, concurrent int, timeout time.Duration) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{processor: p, tempDir: tempDir, maxInput: maxInput, timeout: timeout, slots: make(chan struct{}, concurrent), ctx: ctx, cancel: cancel}
}

// Run may return at a deadline before native work returns. The worker retains
// its slot, source file and image resources until it actually stops. A successful
// result owns the slot until Close, including while its file is streamed out.
func (s *Service) Run(parent context.Context, body io.Reader, options *imageproc.Options) (*Result, error) {
	if err := s.acquire(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	stop := context.AfterFunc(s.ctx, cancel)
	release := func() {
		stop()
		cancel()
		<-s.slots
		s.wg.Done()
	}
	done := make(chan completion)
	go s.run(ctx, body, options, release, done)
	select {
	case outcome := <-done:
		if err := ctx.Err(); err != nil && outcome.err == nil {
			outcome.result.Close()
			return nil, err
		}
		return outcome.result, outcome.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) acquire() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return imageproc.ErrUnavailable
	}
	select {
	case s.slots <- struct{}{}:
		s.wg.Add(1)
		return nil
	default:
		return imageproc.ErrBusy
	}
}

func (s *Service) run(ctx context.Context, body io.Reader, options *imageproc.Options, release func(), done chan<- completion) {
	dir, err := os.MkdirTemp(s.tempDir, "image-transform-")
	cleanup := sync.OnceFunc(func() {
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
		release()
	})
	// The worker owns cleanup until a successful result is accepted by Run.
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	var result *Result
	if err == nil {
		result, err = s.process(ctx, dir, body, options)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		result = nil
	} else {
		result.close = cleanup
	}
	select {
	case done <- completion{result, err}:
		if err == nil {
			cleanup = nil
		}
	case <-ctx.Done():
	}
}

func (s *Service) process(ctx context.Context, dir string, body io.Reader, options *imageproc.Options) (*Result, error) {
	input := filepath.Join(dir, "source")
	f, err := os.OpenFile(input, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), &contextReader{ctx, io.LimitReader(body, s.maxInput+1)})
	closeErr := f.Close()
	if n > s.maxInput {
		return nil, imageproc.ErrTooLarge
	}
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if n == 0 {
		return nil, imageproc.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if options == nil {
		info, err := s.processor.Inspect(ctx, input)
		info.ByteCount, info.SHA256 = n, hex.EncodeToString(hash.Sum(nil))
		return &Result{Metadata: info}, err
	}
	output := filepath.Join(dir, "output")
	info, err := s.processor.Transform(ctx, input, output, *options)
	if err != nil {
		return nil, err
	}
	f, err = os.Open(output)
	if err != nil {
		return nil, err
	}
	hash.Reset()
	info.ByteCount, err = io.Copy(hash, &contextReader{ctx, f})
	_ = f.Close()
	info.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return &Result{Metadata: info, Path: output}, err
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	s.cancel()
}
func (s *Service) Wait() { s.wg.Wait() }
