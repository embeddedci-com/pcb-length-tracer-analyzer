// Package pipehttp serves an http.Handler over a pair of pipes.
//
// It is how the KiCad plugin talks to the engine without a socket. The plugin
// starts the engine as a child process and writes requests to its stdin; the
// engine answers on stdout. Nothing listens on a port, not even loopback, so
// there is nothing for another process on the machine to connect to, and when
// the plugin goes away its end of the pipe closes and the engine exits with it.
//
// The handler is the same one embeddedci.com mounts. Serving it here rather
// than exposing a second, function-call API means the web front end runs in the
// plugin unchanged, and every endpoint the site has, the plugin has.
//
// # Framing
//
// Both directions carry the same frame:
//
//	uint32 big-endian  header length
//	header             JSON
//	uint32 big-endian  body length
//	body               raw bytes
//
// A request header is {"id", "method", "path", "headers"}; a response header is
// {"id", "status", "headers"}. The id is the caller's, echoed back, because
// requests are served concurrently and answered in whatever order they finish:
// a headroom measurement taking a second must not hold up the geometry the
// viewer is waiting for.
package pipehttp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// MaxFrame bounds a header or body. A board file is the largest thing that
// crosses, and the largest real boards are tens of megabytes; a length beyond
// this is a corrupt stream rather than a big board, and reading it would
// allocate whatever garbage the length claimed.
const MaxFrame = 512 << 20

// RequestHeader is the JSON half of a request frame.
type RequestHeader struct {
	ID      uint64            `json:"id"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
}

// ResponseHeader is the JSON half of a response frame.
type ResponseHeader struct {
	ID      uint64            `json:"id"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Serve reads requests from in until it is closed, answering each on out.
//
// It returns nil when in reaches EOF between frames -- the normal way for the
// plugin to say it is done -- and only after every request already read has
// been answered. A stream that ends part-way through a frame, or carries a
// frame that cannot be decoded, is an error: the two ends no longer agree on
// where a frame starts, and nothing after it can be trusted.
func Serve(ctx context.Context, h http.Handler, in io.Reader, out io.Writer) error {
	r := bufio.NewReaderSize(in, 1<<16)
	w := &frameWriter{w: out}
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		hdrRaw, err := readChunk(r)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading a request header: %w", err)
		}
		body, err := readChunk(r)
		if err != nil {
			return fmt.Errorf("reading a request body: %w", unexpected(err))
		}
		var hdr RequestHeader
		if err := json.Unmarshal(hdrRaw, &hdr); err != nil {
			return fmt.Errorf("decoding a request header: %w", err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			status, headers, payload := serveOne(ctx, h, hdr, body)
			// A write failure means the plugin is gone. The read loop will see
			// the same thing on its side and return; there is nobody left to
			// tell.
			_ = w.write(ResponseHeader{ID: hdr.ID, Status: status, Headers: headers}, payload)
		}()
	}
}

func serveOne(ctx context.Context, h http.Handler, hdr RequestHeader, body []byte) (int, map[string]string, []byte) {
	method := strings.ToUpper(hdr.Method)
	if method == "" {
		method = http.MethodGet
	}
	path := hdr.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	req, err := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(body))
	if err != nil {
		return http.StatusBadRequest, jsonType(), errorJSON(fmt.Sprintf("bad request line: %v", err))
	}
	req.RequestURI = path
	req.ContentLength = int64(len(body))
	req.RemoteAddr = "pipe"
	for k, v := range hdr.Headers {
		req.Header.Set(k, v)
	}

	rec := &recorder{header: http.Header{}}
	func() {
		// A panic in one handler must not take the engine down with every
		// other request in flight; answer this one as a server error.
		defer func() {
			if p := recover(); p != nil {
				rec.reset()
				rec.header.Set("Content-Type", "application/json; charset=utf-8")
				rec.WriteHeader(http.StatusInternalServerError)
				_, _ = rec.Write(errorJSON(fmt.Sprintf("the engine failed on %s %s: %v", method, hdr.Path, p)))
			}
		}()
		h.ServeHTTP(rec, req)
	}()
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	headers := make(map[string]string, len(rec.header))
	for k, v := range rec.header {
		if len(v) > 0 {
			headers[k] = strings.Join(v, ", ")
		}
	}
	return rec.status, headers, rec.body.Bytes()
}

// recorder is a ResponseWriter that keeps everything, since the whole response
// goes out as one frame.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
}

func (r *recorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(p)
}

func (r *recorder) reset() {
	r.header = http.Header{}
	r.status = 0
	r.body.Reset()
}

type frameWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (f *frameWriter) write(hdr any, body []byte) error {
	raw, err := json.Marshal(hdr)
	if err != nil {
		return err
	}
	// One frame per write under the lock: two responses finishing together
	// must not interleave their bytes.
	f.mu.Lock()
	defer f.mu.Unlock()
	return WriteFrame(f.w, raw, body)
}

// WriteFrame writes one header and body. Exported for the tests and for any Go
// client that wants to drive the engine the way the plugin does.
func WriteFrame(w io.Writer, header, body []byte) error {
	buf := make([]byte, 0, 8+len(header)+len(body))
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(header)))
	buf = append(buf, header...)
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(body)))
	buf = append(buf, body...)
	_, err := w.Write(buf)
	return err
}

// ReadFrame reads one header and body.
func ReadFrame(r io.Reader) (header, body []byte, err error) {
	header, err = readChunk(r)
	if err != nil {
		return nil, nil, err
	}
	body, err = readChunk(r)
	if err != nil {
		return nil, nil, unexpected(err)
	}
	return header, body, nil
}

func readChunk(r io.Reader) ([]byte, error) {
	var n [4]byte
	// ReadFull returns io.EOF only when nothing at all was read, which is the
	// one place a stream may cleanly end.
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(n[:])
	if size > MaxFrame {
		return nil, fmt.Errorf("a frame of %d bytes is over the %d byte limit", size, MaxFrame)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, unexpected(err)
	}
	return buf, nil
}

// unexpected turns a clean EOF inside a frame into the error it really is.
func unexpected(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}

func jsonType() map[string]string {
	return map[string]string{"Content-Type": "application/json; charset=utf-8"}
}

func errorJSON(msg string) []byte {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return b
}
