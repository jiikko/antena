package store

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// DefaultStaleAfter: a lock older than this is treated as left behind by a
// killed execution. Keep it longer than the Cloud Run task timeout
// (release.yml sets 30m): a live execution must be killed by Cloud Run before
// another one may take its lock over, or two executions would write the state
// concurrently.
const DefaultStaleAfter = time.Hour

// Lock is the run lock in docs/state-format.md ("ロック"). Any other
// implementation that follows that protocol excludes this one when they share
// a prefix.
type Lock struct {
	store      *Store
	warn       func(string)
	staleAfter time.Duration
	now        func() time.Time
	token      string
	acquired   bool
}

func NewLock(s *Store, warn func(string), staleAfter time.Duration, now func() time.Time) *Lock {
	return &Lock{store: s, warn: warn, staleAfter: staleAfter, now: now, token: uuid()}
}

// Acquire returns false (after a WARN) when another execution holds the lock.
func (l *Lock) Acquire(ctx context.Context) (bool, error) {
	attempts := 0
	for {
		err := l.put(ctx, func(in *s3.PutObjectInput) { in.IfNoneMatch = aws.String("*") })
		if err == nil {
			l.acquired = true
			return true, nil
		}
		status := statusCode(err)
		if status != 409 && status != 412 {
			return false, err
		}
		current, etag, rerr := l.read(ctx)
		if rerr != nil {
			return false, rerr
		}
		if current != nil && current["token"] == l.token {
			// A successful PUT whose response was lost may be retried by the SDK.
			l.acquired = true
			return true, nil
		}
		if attempts < 2 && (status == 409 || current == nil) {
			attempts++
			continue
		}
		if current == nil {
			return false, err
		}
		if l.stale(current) {
			took, terr := l.takeOver(ctx, current, etag)
			if terr != nil {
				return false, terr
			}
			if took {
				return true, nil
			}
			// Another execution replaced the stale lock first; re-read it.
			if attempts < 2 {
				attempts++
				continue
			}
		}
		l.warn(fmt.Sprintf("WARN migration-next skipped: another execution owns the S3 lock (started_at=%s)", startedAt(current)))
		return false, nil
	}
}

// Release deletes the lock if this execution still owns it. Not conditional:
// a takeover between the read and this delete needs this execution to have
// held the lock past the stale age, which the Cloud Run task timeout rules
// out. A manual run with the live (empty) prefix has no such limit; run it
// only while the scheduled job is paused.
func (l *Lock) Release(ctx context.Context) {
	if !l.acquired {
		return
	}
	current, _, err := l.read(ctx)
	if err == nil && (current == nil || current["token"] != l.token) {
		return
	}
	if err == nil {
		key := l.store.LockKey()
		_, err = l.store.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &l.store.Bucket, Key: &key})
	}
	if err != nil {
		l.warn(fmt.Sprintf("WARN migration-next lock release failed: %T: %v; lock may need manual cleanup", err, err))
		return
	}
	l.acquired = false
}

func (l *Lock) put(ctx context.Context, condition func(*s3.PutObjectInput)) error {
	body, _ := json.Marshal(map[string]string{"token": l.token, "started_at": l.now().UTC().Format("2006-01-02T15:04:05.000000Z07:00")})
	key := l.store.LockKey()
	in := &s3.PutObjectInput{Bucket: &l.store.Bucket, Key: &key, Body: strings.NewReader(string(body)),
		ContentType: aws.String("application/json; charset=utf-8")}
	condition(in)
	_, err := l.store.Client.PutObject(ctx, in)
	return err
}

// takeOver replaces the stale lock only if it is still the object read
// (If-Match), so at most one of several waiting executions takes it over.
func (l *Lock) takeOver(ctx context.Context, current map[string]any, etag string) (bool, error) {
	if etag == "" {
		return false, nil
	}
	err := l.put(ctx, func(in *s3.PutObjectInput) { in.IfMatch = aws.String(etag) })
	if err != nil {
		// 404: the stale lock was deleted after it was read; 409/412: someone else replaced it.
		switch statusCode(err) {
		case 404, 409, 412:
			return false, nil
		}
		return false, err
	}
	l.acquired = true
	l.warn(fmt.Sprintf("WARN migration-next took over a stale S3 lock (started_at=%s, stale_after=%ds)", startedAt(current), int(l.staleAfter.Seconds())))
	return true, nil
}

func (l *Lock) stale(current map[string]any) bool {
	s, _ := current["started_at"].(string)
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		// An unreadable start time can never expire on its own.
		l.warn(fmt.Sprintf("WARN migration-next lock has an unreadable started_at=%q; treating it as stale", s))
		return true
	}
	return l.now().Sub(t) > l.staleAfter
}

// read returns nil when no lock exists. A body that is not a JSON object
// comes back as an empty map (no token, no started_at), so it is treated as
// stale and taken over instead of blocking every run.
func (l *Lock) read(ctx context.Context) (map[string]any, string, error) {
	key := l.store.LockKey()
	out, err := l.store.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: &l.store.Bucket, Key: &key})
	if err != nil {
		if isNoSuchKey(err) {
			return nil, "", nil
		}
		return nil, "", err
	}
	defer out.Body.Close()
	body, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, "", err
	}
	var parsed map[string]any
	if json.Unmarshal(body, &parsed) != nil || parsed == nil {
		l.warn(fmt.Sprintf("WARN migration-next lock body is not a JSON object (%d bytes)", len(body)))
		parsed = map[string]any{}
	}
	return parsed, aws.ToString(out.ETag), nil
}

func startedAt(current map[string]any) string {
	if s, ok := current["started_at"].(string); ok {
		return s
	}
	return "unknown"
}

func statusCode(err error) int {
	var re *smithyhttp.ResponseError
	if errors.As(err, &re) {
		return re.HTTPStatusCode()
	}
	return 0
}

func isNoSuchKey(err error) bool {
	var missing interface{ ErrorCode() string }
	return errors.As(err, &missing) && missing.ErrorCode() == "NoSuchKey"
}

func uuid() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
