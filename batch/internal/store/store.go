// Package store holds the S3 keys of one run (pages, post state, lock) and
// the run lock. Keys and the lock protocol are specified in
// migration-next/state-format.md and shared with the Ruby implementation.
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

// FromEnv reads MIGRATION_NEXT_BUCKET and MIGRATION_NEXT_PREFIX. An empty
// prefix writes the live site and needs MIGRATION_NEXT_ALLOW_PUBLIC=true.
func FromEnv(getenv func(string) string, lookup func(string) (string, bool), client API) (*Store, error) {
	bucket, ok := lookup("MIGRATION_NEXT_BUCKET")
	if !ok {
		bucket = "prpr-antena"
	}
	prefix, ok := lookup("MIGRATION_NEXT_PREFIX")
	if !ok {
		prefix = "migration-preview"
	}
	prefix = slashes.ReplaceAllString(prefix, "")
	if prefix == "" && getenv("MIGRATION_NEXT_ALLOW_PUBLIC") != "true" {
		return nil, errors.New("refusing to write public S3 keys; set MIGRATION_NEXT_ALLOW_PUBLIC=true only for cutover")
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
func (s *Store) LockKey() string  { return join("locks", s.Prefix, "migration-next-run.lock") }

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
