// Package s3fake is an in-memory S3 behind an HTTP server, for tests. It
// answers the real SDK client the way S3 does for the calls the batch makes
// (GetObject, PutObject with If-None-Match / If-Match, DeleteObject), so
// error mapping is tested through the SDK instead of a mocked interface.
package s3fake

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Object struct {
	Body        string
	ETag        string
	ContentType string
}

// Answer is one scripted PUT response.
type Answer struct {
	Status int
	Store  bool // store the body before answering Status
}

type Server struct {
	mu      sync.Mutex
	Objects map[string]Object // key without the bucket
	Puts    []string          // keys of successful PUTs, in order
	Gets    []string          // keys of GETs, in order
	// GetStatus, when set for a key, answers GETs of it with that status.
	GetStatus map[string]int
	// PutStatus, when set for a key, answers PUTs of it with that status.
	PutStatus map[string]int
	// PutOnce answers the next PUTs of a key, one entry per PUT, before the
	// usual handling.
	PutOnce map[string][]Answer
	srv     *httptest.Server
}

func New() *Server {
	s := &Server{Objects: map[string]Object{}, GetStatus: map[string]int{}, PutStatus: map[string]int{}, PutOnce: map[string][]Answer{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

func (s *Server) Close() { s.srv.Close() }

// Client returns a real SDK client pointed at the fake (path-style).
func (s *Server) Client() *s3.Client {
	return s3.New(s3.Options{
		Region:           "ap-northeast-1",
		BaseEndpoint:     aws.String(s.srv.URL),
		UsePathStyle:     true,
		Credentials:      credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""),
		RetryMaxAttempts: 1,
	})
}

func (s *Server) Object(key string) (Object, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.Objects[key]
	return o, ok
}

func (s *Server) Put(key, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Objects[key] = Object{Body: body, ETag: etag(body)}
}

func etag(body string) string {
	sum := md5.Sum([]byte(body))
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	if len(parts) != 2 {
		fail(w, 400, "InvalidRequest")
		return
	}
	key := parts[1]
	current, exists := s.Objects[key]
	switch r.Method {
	case http.MethodGet:
		s.Gets = append(s.Gets, key)
		if st := s.GetStatus[key]; st != 0 {
			fail(w, st, http.StatusText(st))
			return
		}
		if !exists {
			fail(w, 404, "NoSuchKey")
			return
		}
		w.Header().Set("ETag", current.ETag)
		w.Header().Set("Content-Length", fmt.Sprint(len(current.Body)))
		_, _ = io.WriteString(w, current.Body)
	case http.MethodPut:
		if q := s.PutOnce[key]; len(q) > 0 {
			a := q[0]
			s.PutOnce[key] = q[1:]
			if a.Store {
				// The write happened, but the client sees a failure (a lost response).
				body, _ := io.ReadAll(r.Body)
				s.Objects[key] = Object{Body: string(body), ETag: etag(string(body)), ContentType: r.Header.Get("Content-Type")}
			}
			fail(w, a.Status, http.StatusText(a.Status))
			return
		}
		if st := s.PutStatus[key]; st != 0 {
			fail(w, st, http.StatusText(st))
			return
		}
		if r.Header.Get("If-None-Match") == "*" && exists {
			fail(w, 412, "PreconditionFailed")
			return
		}
		if m := r.Header.Get("If-Match"); m != "" {
			if !exists {
				fail(w, 404, "NoSuchKey") // real S3 answers If-Match on a missing key with 404
				return
			}
			if m != current.ETag {
				fail(w, 412, "PreconditionFailed")
				return
			}
		}
		body, _ := io.ReadAll(r.Body)
		o := Object{Body: string(body), ETag: etag(string(body)), ContentType: r.Header.Get("Content-Type")}
		s.Objects[key] = o
		s.Puts = append(s.Puts, key)
		w.Header().Set("ETag", o.ETag)
	case http.MethodDelete:
		delete(s.Objects, key)
		w.WriteHeader(204)
	default:
		fail(w, 405, "MethodNotAllowed")
	}
}

func fail(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, code, code)
}
