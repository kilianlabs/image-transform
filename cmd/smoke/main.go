// Command smoke verifies the real server and shared libraries in the final image.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/davidbyttow/govips/v2/vips"
	"github.com/kilianlabs/image-transform/internal/testimage"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if os.Getuid() == 0 {
		return fmt.Errorf("runtime must not run as root")
	}
	vips.LoggingSettings(func(string, vips.LogLevel, string) {}, vips.LogLevelError)
	if err := vips.Startup(&vips.Config{ConcurrencyLevel: 2, MaxCacheMem: 32 << 20, MaxCacheSize: 0}); err != nil {
		return err
	}
	defer vips.Shutdown()
	server := exec.Command("image-transform")
	server.Stdout = os.Stdout
	server.Stderr = os.Stderr
	if err := server.Start(); err != nil {
		return err
	}
	defer server.Process.Kill()
	exit := make(chan error, 1)
	go func() { exit <- server.Wait() }()
	client := &http.Client{Timeout: 10 * time.Second}
	base := "http://127.0.0.1:3000"
	ready := false
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); {
		select {
		case err := <-exit:
			return fmt.Errorf("server exited before readiness: %v", err)
		default:
		}
		resp, err := client.Get(base + "/health")
		if err == nil {
			resp.Body.Close()
			ready = resp.StatusCode == 200
		}
		if ready {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		return fmt.Errorf("server did not become ready")
	}
	response, err := client.Get(base + "/api/openapi.json")
	if err != nil {
		return err
	}
	contract, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		return err
	}
	saved, err := os.ReadFile("/app/openapi.json")
	if err != nil {
		return err
	}
	if !bytes.Equal(contract, saved) {
		return fmt.Errorf("served OpenAPI differs from checked-in contract")
	}
	type fixture struct {
		name          string
		width, height int
		alpha         bool
		animated      bool
		data          []byte
		mime          string
		pages, page   int
	}
	fixtures := []fixture{
		{name: "gradient-text", width: 768, height: 512},
		{name: "transparency", width: 96, height: 64, alpha: true},
		{name: "tiny", width: 1, height: 1},
		{name: "portrait", width: 48, height: 96, alpha: true},
		{name: "animation", width: 480, height: 320, alpha: true, animated: true},
	}
	for _, f := range []fixture{
		{name: "sample.bmp", mime: "image/bmp"},
		{name: "sample.tiff", mime: "image/tiff", alpha: true, pages: 1},
		{name: "sample.heic", mime: "image/heic", alpha: true, pages: 1},
		{name: "sample.heif", mime: "image/heif", alpha: true, pages: 1},
		{name: "sample.avif", mime: "image/avif", alpha: true, pages: 1},
		{name: "collection.tiff", mime: "image/tiff", pages: 2},
		{name: "collection.heic", mime: "image/heic", pages: 2, page: 1},
		{name: "collection.avif", mime: "image/avif", pages: 2, page: 1},
	} {
		f.data, err = os.ReadFile(filepath.Join("/app/testdata", f.name))
		if err != nil {
			return err
		}
		f.width, f.height = 24, 16
		fixtures = append(fixtures, f)
	}
	for _, fixture := range fixtures {
		input := fixture.data
		if input == nil {
			input = testimage.PNG(fixture.width, fixture.height, fixture.alpha)
		}
		frames := 1
		if fixture.animated {
			input = testimage.GIF(fixture.width, fixture.height, []int{4, 7, 13}, 2)
			frames = 3
		}
		hash := sha256.Sum256(input)
		resp, err := client.Post(base+"/api/inspect", "application/octet-stream", bytes.NewReader(input))
		if err != nil {
			return err
		}
		var metadata struct {
			MIMEType      string `json:"mime_type"`
			SHA256        string `json:"sha256"`
			Width         int    `json:"width"`
			FrameCount    int    `json:"frame_count"`
			FrameDelaysMS []int  `json:"frame_delays_ms"`
			LoopCount     *int   `json:"loop_count"`
			PageCount     int    `json:"page_count"`
			PageIndex     *int   `json:"page_index"`
		}
		err = json.NewDecoder(resp.Body).Decode(&metadata)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode != 200 || metadata.SHA256 != fmt.Sprintf("%x", hash) || metadata.Width != fixture.width || metadata.FrameCount != frames {
			return fmt.Errorf("inspection failed: %d %+v", resp.StatusCode, metadata)
		}
		if fixture.mime != "" && metadata.MIMEType != fixture.mime {
			return fmt.Errorf("incorrect input MIME type: %+v", metadata)
		}
		if fixture.pages > 0 && (metadata.PageCount != fixture.pages || metadata.PageIndex == nil || *metadata.PageIndex != fixture.page) {
			return fmt.Errorf("incorrect collection selection: %+v", metadata)
		}
		for _, format := range []string{"webp", "jxl"} {
			resp, err = client.Post(base+"/api/transform?format="+format+"&width=320&height=320", "application/octet-stream", bytes.NewReader(input))
			if err != nil {
				return err
			}
			encoded, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return err
			}
			if resp.StatusCode != 200 {
				return fmt.Errorf("transform failed: %d %s", resp.StatusCode, encoded)
			}
			if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Content-Type") != "image/"+format || resp.ContentLength != int64(len(encoded)) {
				return fmt.Errorf("invalid binary response headers")
			}
			if resp.Header.Get("X-Image-SHA256") != fmt.Sprintf("%x", sha256.Sum256(encoded)) || resp.Header.Get("X-Image-Frame-Count") != strconv.Itoa(frames) {
				return fmt.Errorf("output checksum mismatch")
			}
			decoded, err := vips.NewImageFromBuffer(encoded)
			if err != nil {
				return err
			}
			width, height := decoded.Width(), decoded.Height()
			_, err = decoded.Average()
			alpha := decoded.HasAlpha()
			decoded.Close()
			if err != nil {
				return err
			}
			if width > 320 || height > 320 || width > fixture.width || height > fixture.height || (fixture.alpha && !alpha) || resp.Header.Get("X-Image-Width") != strconv.Itoa(width) || resp.Header.Get("X-Image-Height") != strconv.Itoa(height) {
				return fmt.Errorf("invalid derivative dimensions or alpha")
			}
			if fixture.animated {
				inspected, err := client.Post(base+"/api/inspect", "application/octet-stream", bytes.NewReader(encoded))
				if err != nil {
					return err
				}
				err = json.NewDecoder(inspected.Body).Decode(&metadata)
				inspected.Body.Close()
				if err != nil {
					return err
				}
				if inspected.StatusCode != 200 || metadata.FrameCount != 3 || metadata.LoopCount == nil || *metadata.LoopCount != 3 || !slices.Equal(metadata.FrameDelaysMS, []int{40, 70, 130}) {
					return fmt.Errorf("animation metadata lost: %+v", metadata)
				}
			}
			fmt.Printf("Smoke test passed: %s -> %s (%dx%d).\n", fixture.name, format, width, height)
		}
	}
	for _, test := range []struct {
		path, body, media string
		status            int
	}{{"/api/transform?format=gif", "bad", "application/octet-stream", 400}, {"/api/inspect", "<svg>unsupported</svg>", "application/octet-stream", 415}, {"/api/inspect", "bad", "image/png", 415}} {
		resp, err := client.Post(base+test.path, test.media, bytes.NewBufferString(test.body))
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != test.status {
			return fmt.Errorf("validation status %d != %d", resp.StatusCode, test.status)
		}
	}
	// A body that never finishes must not prevent graceful container shutdown.
	stalled, err := net.Dial("tcp", "127.0.0.1:3000")
	if err != nil {
		return err
	}
	defer stalled.Close()
	fmt.Fprint(stalled, "POST /api/inspect HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/octet-stream\r\nContent-Length: 100\r\n\r\nx")
	if err := server.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	select {
	case err := <-exit:
		if err != nil {
			return err
		}
	case <-time.After(9 * time.Second):
		return fmt.Errorf("shutdown did not complete")
	}
	fmt.Println("Container smoke passed.")
	return nil
}
