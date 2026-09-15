package pipehttp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// client drives Serve the way the plugin does: frames in, frames out.
type client struct {
	t    *testing.T
	in   *io.PipeWriter
	out  *io.PipeReader
	done chan error
}

func start(t *testing.T, h http.Handler) *client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	c := &client{t: t, in: inW, out: outR, done: make(chan error, 1)}
	go func() {
		err := Serve(context.Background(), h, inR, outW)
		_ = outW.Close()
		c.done <- err
	}()
	return c
}

func (c *client) send(id uint64, method, path string, headers map[string]string, body []byte) {
	c.t.Helper()
	hdr, _ := json.Marshal(RequestHeader{ID: id, Method: method, Path: path, Headers: headers})
	if err := WriteFrame(c.in, hdr, body); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

func (c *client) recv() (ResponseHeader, []byte) {
	c.t.Helper()
	raw, body, err := ReadFrame(c.out)
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	var hdr ResponseHeader
	if err := json.Unmarshal(raw, &hdr); err != nil {
		c.t.Fatalf("decode: %v", err)
	}
	return hdr, body
}

func TestRoundTripKeepsMethodPathQueryHeadersAndBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/things/{id}", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Seen", r.Header.Get("X-Token"))
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id": r.PathValue("id"), "q": r.URL.Query().Get("q"), "body": string(b),
		})
	})
	c := start(t, mux)
	c.send(7, "POST", "/api/things/abc?q=/ddr4/DQ0", map[string]string{"X-Token": "t1"}, []byte("hello"))
	hdr, body := c.recv()
	if hdr.ID != 7 || hdr.Status != http.StatusCreated {
		t.Fatalf("header = %+v", hdr)
	}
	if hdr.Headers["X-Seen"] != "t1" {
		t.Errorf("request header did not reach the handler: %+v", hdr.Headers)
	}
	var got map[string]string
	_ = json.Unmarshal(body, &got)
	if got["id"] != "abc" || got["q"] != "/ddr4/DQ0" || got["body"] != "hello" {
		t.Errorf("handler saw %+v", got)
	}
	_ = c.in.Close()
	if err := <-c.done; err != nil {
		t.Fatalf("clean close returned %v", err)
	}
}

// A slow request must not hold up a fast one, and every answer must carry the
// id of the question it answers.
func TestConcurrentRequestsAnswerOutOfOrderByID(t *testing.T) {
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = w.Write([]byte("slow"))
	})
	mux.HandleFunc("GET /fast", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fast"))
	})
	c := start(t, mux)
	c.send(1, "GET", "/slow", nil, nil)
	c.send(2, "GET", "/fast", nil, nil)
	hdr, body := c.recv()
	if hdr.ID != 2 || string(body) != "fast" {
		t.Fatalf("first answer = id %d %q, want the fast one", hdr.ID, body)
	}
	close(release)
	hdr, body = c.recv()
	if hdr.ID != 1 || string(body) != "slow" {
		t.Fatalf("second answer = id %d %q", hdr.ID, body)
	}
	_ = c.in.Close()
	<-c.done
}

// Closing stdin while a request is in flight still gets that request answered:
// the plugin may close as soon as it has sent its last question.
func TestCloseWaitsForInFlightRequests(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	})
	c := start(t, mux)
	c.send(1, "GET", "/x", nil, nil)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		hdr, body := c.recv()
		if hdr.ID != 1 || string(body) != "done" {
			t.Errorf("got %+v %q", hdr, body)
		}
	}()
	_ = c.in.Close()
	wg.Wait()
	if err := <-c.done; err != nil {
		t.Fatal(err)
	}
}

func TestPanicInHandlerIsA500NotACrash(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Partial", "yes")
		panic("kaboom")
	})
	mux.HandleFunc("GET /ok", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	c := start(t, mux)
	c.send(1, "GET", "/boom", nil, nil)
	hdr, body := c.recv()
	if hdr.Status != 500 || !strings.Contains(string(body), "kaboom") {
		t.Fatalf("got %d %s", hdr.Status, body)
	}
	if hdr.Headers["X-Partial"] != "" {
		t.Error("headers from the failed handler leaked into the 500")
	}
	c.send(2, "GET", "/ok", nil, nil)
	if hdr, _ := c.recv(); hdr.Status != 200 {
		t.Fatalf("engine did not survive the panic: %+v", hdr)
	}
	_ = c.in.Close()
	<-c.done
}

func TestUnknownRouteIs404(t *testing.T) {
	c := start(t, http.NewServeMux())
	c.send(1, "GET", "nowhere", nil, nil)
	if hdr, _ := c.recv(); hdr.Status != 404 {
		t.Fatalf("status %d", hdr.Status)
	}
	_ = c.in.Close()
	<-c.done
}

func TestTruncatedFrameIsAnError(t *testing.T) {
	var in bytes.Buffer
	_ = binary.Write(&in, binary.BigEndian, uint32(100))
	in.WriteString("{\"id\":1")
	err := Serve(context.Background(), http.NewServeMux(), &in, io.Discard)
	if err == nil {
		t.Fatal("a stream cut mid-frame was treated as a clean close")
	}
}

func TestOversizedFrameIsRefusedBeforeAllocating(t *testing.T) {
	var in bytes.Buffer
	_ = binary.Write(&in, binary.BigEndian, uint32(MaxFrame+1))
	err := Serve(context.Background(), http.NewServeMux(), &in, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("err = %v", err)
	}
}
