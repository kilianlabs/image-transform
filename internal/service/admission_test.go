package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kilianlabs/image-transform/internal/imageproc"
)

func TestAdmissionLimits(t *testing.T) {
	for _, test := range []struct {
		name       string
		concurrent int
	}{{"shared limit", 1}, {"independent capacity", 2}} {
		t.Run(test.name, func(t *testing.T) {
			entered, unblock := make(chan struct{}), make(chan struct{})
			finish := sync.OnceFunc(func() { close(unblock) })
			p := processor{
				inspect: func(context.Context, string) (imageproc.Metadata, error) { return imageproc.Metadata{}, nil },
				transform: func(_ context.Context, _, destination string, _ imageproc.Options) (imageproc.Metadata, error) {
					close(entered)
					<-unblock
					return imageproc.Metadata{}, os.WriteFile(destination, []byte("encoded"), 0600)
				},
			}
			s := New(p, t.TempDir(), 100, AdmissionLimits{Concurrent: test.concurrent, InspectConcurrent: 1, TransformConcurrent: 1}, time.Minute)
			t.Cleanup(func() { finish(); s.Stop(); s.Wait() })
			done := make(chan error, 1)
			go func() {
				result, err := s.Run(t.Context(), strings.NewReader("conversion"), &imageproc.Options{})
				if result != nil {
					result.Close()
				}
				done <- err
			}()
			<-entered

			// Conversion cannot borrow inspection capacity, and rejected bodies stay unread.
			body := &trackedBody{Reader: strings.NewReader("rejected")}
			if _, err := s.Run(t.Context(), body, &imageproc.Options{}); !errors.Is(err, imageproc.ErrBusy) || body.read.Load() {
				t.Fatal("conversion exceeded its limit or read a rejected body", err)
			}
			result, err := s.Run(t.Context(), strings.NewReader("inspection"), nil)
			if result != nil {
				defer result.Close()
			}
			if test.concurrent == 1 && !errors.Is(err, imageproc.ErrBusy) {
				t.Fatal("overall limit was exceeded", err)
			}
			if test.concurrent == 2 && err != nil {
				t.Fatal("inspection could not run alongside conversion", err)
			}
			finish()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if result != nil {
				// Even with conversion finished, a second inspection cannot borrow its slot.
				if _, err := s.Run(t.Context(), strings.NewReader("another inspection"), nil); !errors.Is(err, imageproc.ErrBusy) {
					t.Fatal("inspection exceeded its limit", err)
				}
			}
		})
	}
}

type trackedBody struct {
	*strings.Reader
	read atomic.Bool
}

func (r *trackedBody) Read(p []byte) (int, error) { r.read.Store(true); return r.Reader.Read(p) }
