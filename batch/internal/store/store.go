// Package store holds the S3 keys of one run (pages, post state, lock) and
// the run lock. Keys and the lock protocol are specified in
// docs/state-format.md.
package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// API is the part of the S3 client the batch uses.
type API interface {
	GetObject(ctx context.Context, in *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, opts ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type Store struct {
	Client API
	Bucket string
	Prefix string // without leading or trailing "/"; "" writes the live keys
}

var slashes = regexp.MustCompile(`\A/+|/+\z`)

// FromEnv reads ANTENA_BUCKET and ANTENA_PREFIX. ANTENA_PREFIX has no
// fallback: a run that forgot it stops instead of writing somewhere. An empty
// prefix writes the live site and needs ANTENA_ALLOW_PUBLIC=true; a trial run
// uses "preview", which the bucket lifecycle expires after a day.
func FromEnv(getenv func(string) string, lookup func(string) (string, bool), client API) (*Store, error) {
	bucket, ok := lookup("ANTENA_BUCKET")
	if !ok {
		bucket = "prpr-antena"
	}
	prefix, ok := lookup("ANTENA_PREFIX")
	if !ok {
		return nil, errors.New(`ANTENA_PREFIX is not set; set it to "" (with ANTENA_ALLOW_PUBLIC=true) for the live site or to "preview" for a trial run`)
	}
	prefix = slashes.ReplaceAllString(prefix, "")
	if prefix == "" && getenv("ANTENA_ALLOW_PUBLIC") != "true" {
		return nil, errors.New("refusing to write public S3 keys; set ANTENA_ALLOW_PUBLIC=true only for cutover")
	}
	return &Store{Client: client, Bucket: bucket, Prefix: prefix}, nil
}

// Key is a page key below the prefix.
func (s *Store) Key(relative string) string {
	return join(s.Prefix, strings.TrimLeft(relative, "/"))
}

// state/ and locks/ stay at the top so the bucket policy Deny on state/* and
// locks/* covers them; a non-empty prefix goes below them.
func (s *Store) StateKey() string { return join("state", s.Prefix, "posts.json") }
func (s *Store) LockKey() string  { return join("locks", s.Prefix, "antena-run.lock") }

func join(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "/")
}

// ReadIfExists returns nil, nil only for a missing key. Any other failure
// (AccessDenied, network) is an error, so a caller never mistakes an
// unreadable object for an absent one.
func (s *Store) ReadIfExists(ctx context.Context, key string) ([]byte, error) {
	out, err := s.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.Bucket, Key: &key})
	if err != nil {
		if isNoSuchKey(err) {
			return nil, nil
		}
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (s *Store) Write(ctx context.Context, key string, body []byte, contentType string) error {
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.Bucket, Key: &key, Body: bytes.NewReader(body), ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("put s3://%s/%s: %w", s.Bucket, key, err)
	}
	return nil
}
