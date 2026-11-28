package store

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jiikko/antena/batch/internal/s3fake"
)

func env(m map[string]string) (func(string) string, func(string) (string, bool)) {
	return func(k string) string { return m[k] }, func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// The live run keeps state/posts.json and its lock where they have always
// been; a run under a page prefix gets its own, still below state/ and locks/.
func TestKeys(t *testing.T) {
	cases := []struct {
		env               map[string]string
		state, lock, page string
	}{
		{map[string]string{"MIGRATION_NEXT_PREFIX": "", "MIGRATION_NEXT_ALLOW_PUBLIC": "true"}, "state/posts.json", "locks/migration-next-run.lock", "index.html"},
		{map[string]string{"MIGRATION_NEXT_PREFIX": "/go-preview/"}, "state/go-preview/posts.json", "locks/go-preview/migration-next-run.lock", "go-preview/index.html"},
		{map[string]string{}, "state/migration-preview/posts.json", "locks/migration-preview/migration-next-run.lock", "migration-preview/index.html"},
	}
	for _, c := range cases {
		get, look := env(c.env)
		s, err := FromEnv(get, look, nil)
		if err != nil {
			t.Fatal(err)
		}
		if s.StateKey() != c.state || s.LockKey() != c.lock || s.Key("index.html") != c.page {
			t.Errorf("%v: %s %s %s", c.env, s.StateKey(), s.LockKey(), s.Key("index.html"))
		}
	}
}

func TestEmptyPrefixNeedsAllowPublic(t *testing.T) {
	for _, p := range []string{"", "/", "//"} {
		get, look := env(map[string]string{"MIGRATION_NEXT_PREFIX": p})
		if _, err := FromEnv(get, look, nil); err == nil || !strings.Contains(err.Error(), "refusing to write public S3 keys") {
			t.Errorf("prefix %q: err = %v", p, err)
		}
	}
}

func TestReadIfExists(t *testing.T) {
	fake := s3fake.New()
	defer fake.Close()
	s := &Store{Client: fake.Client(), Bucket: "b", Prefix: "p"}
	ctx := context.Background()
	if data, err := s.ReadIfExists(ctx, "missing"); data != nil || err != nil {
		t.Fatalf("missing key: %q %v", data, err)
	}
	fake.GetStatus["denied"] = 403
	if _, err := s.ReadIfExists(ctx, "denied"); err == nil {
		t.Fatal("AccessDenied must be an error, not a missing key")
	}
	fake.Put("there", "body")
	if data, err := s.ReadIfExists(ctx, "there"); string(data) != "body" || err != nil {
		t.Fatalf("existing key: %q %v", data, err)
	}
}

type lockEnv struct {
	t     *testing.T
	fake  *s3fake.Server
	store *Store
	warns bytes.Buffer
	t0    time.Time
}

func newLockEnv(t *testing.T) *lockEnv {
	fake := s3fake.New()
	t.Cleanup(fake.Close)
	return &lockEnv{t: t, fake: fake, store: &Store{Client: fake.Client(), Bucket: "page-bucket", Prefix: "shadow"},
		t0: time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)}
}

func (e *lockEnv) at(d time.Duration) *Lock {
	now := e.t0.Add(d)
	return NewLock(e.store, func(s string) { e.warns.WriteString(s + "\n") }, time.Hour, func() time.Time { return now })
}

func (e *lockEnv) acquire(l *Lock) bool {
	ok, err := l.Acquire(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return ok
}

func (e *lockEnv) ownerStartedAt() string {
	o, ok := e.fake.Object(e.store.LockKey())
	if !ok {
		e.t.Fatal("no lock object")
	}
	var body map[string]string
	_ = json.Unmarshal([]byte(o.Body), &body)
	return body["started_at"]
}

func TestLockSkipsWhileFreshLockIsHeld(t *testing.T) {
	e := newLockEnv(t)
	if !e.acquire(e.at(0)) || e.acquire(e.at(time.Minute)) {
		t.Fatal("want first true, second false")
	}
	if !strings.Contains(e.warns.String(), "WARN migration-next skipped") {
		t.Fatal(e.warns.String())
	}
}

func TestLockTakesOverStaleLock(t *testing.T) {
	e := newLockEnv(t)
	if !e.acquire(e.at(0)) || !e.acquire(e.at(time.Hour+time.Second)) {
		t.Fatal("want both true")
	}
	if got := e.ownerStartedAt(); got != "2026-11-26T01:00:01.000000Z" {
		t.Fatal(got)
	}
	if !strings.Contains(e.warns.String(), "WARN migration-next took over a stale S3 lock") {
		t.Fatal(e.warns.String())
	}
}

func TestLockSecondWaiterWithOutdatedETagDoesNotTakeOver(t *testing.T) {
	e := newLockEnv(t)
	e.acquire(e.at(0))
	stale, _ := e.fake.Object(e.store.LockKey())
	e.acquire(e.at(time.Hour + time.Second))
	late := e.at(time.Hour + 2*time.Second)
	var current map[string]any
	_ = json.Unmarshal([]byte(stale.Body), &current)
	took, err := late.takeOver(context.Background(), current, stale.ETag)
	if err != nil || took {
		t.Fatalf("took=%v err=%v", took, err)
	}
	if got := e.ownerStartedAt(); got != "2026-11-26T01:00:01.000000Z" {
		t.Fatal(got)
	}
}

func TestLockDoesNotDeleteLockTakenOver(t *testing.T) {
	e := newLockEnv(t)
	original := e.at(0)
	e.acquire(original)
	e.acquire(e.at(time.Hour + time.Second))
	original.Release(context.Background())
	if got := e.ownerStartedAt(); got != "2026-11-26T01:00:01.000000Z" {
		t.Fatal(got)
	}
}

func TestLockUnreadableStartedAtIsStale(t *testing.T) {
	e := newLockEnv(t)
	e.fake.Put(e.store.LockKey(), `{"token":"x","started_at":"garbage"}`)
	if !e.acquire(e.at(0)) || !strings.Contains(e.warns.String(), "unreadable started_at") {
		t.Fatal(e.warns.String())
	}
}

func TestLockBodyNotJSONObjectIsTakenOver(t *testing.T) {
	e := newLockEnv(t)
	e.fake.Put(e.store.LockKey(), "")
	if !e.acquire(e.at(0)) || !strings.Contains(e.warns.String(), "not a JSON object") {
		t.Fatal(e.warns.String())
	}
}

func TestLockTakeOverGivesUpWhenStaleLockDisappeared(t *testing.T) {
	e := newLockEnv(t)
	took, err := e.at(time.Hour+time.Second).takeOver(context.Background(), map[string]any{"started_at": "2026-11-26T00:00:00Z"}, `"gone"`)
	if err != nil || took {
		t.Fatalf("took=%v err=%v", took, err)
	}
	if _, ok := e.fake.Object(e.store.LockKey()); ok {
		t.Fatal("lock must not be created")
	}
}

func TestLockReleasesOwnLock(t *testing.T) {
	e := newLockEnv(t)
	l := e.at(0)
	e.acquire(l)
	l.Release(context.Background())
	if _, ok := e.fake.Object(e.store.LockKey()); ok {
		t.Fatal("lock still there")
	}
}

// Other PUT failures (AccessDenied, server errors) fail the run instead of
// being taken for a held lock.
func TestLockOtherPutErrorsFail(t *testing.T) {
	e := newLockEnv(t)
	e.fake.PutStatus[e.store.LockKey()] = 403
	if _, err := e.at(0).Acquire(context.Background()); err == nil {
		t.Fatal("want an error")
	}
}

// S3 answers 409 (ConditionalRequestConflict) to a conditional write that
// races another one: the lock retries instead of failing or skipping.
func TestLockRetriesAfterConflict(t *testing.T) {
	e := newLockEnv(t)
	e.fake.PutOnce[e.store.LockKey()] = []s3fake.Answer{{Status: 409}}
	if !e.acquire(e.at(0)) {
		t.Fatal("want the retry to take the lock")
	}
	if _, ok := e.fake.Object(e.store.LockKey()); !ok {
		t.Fatal("no lock object")
	}
}

// A PUT whose response was lost: the lock object is ours, so the run holds
// the lock (and releases it) instead of skipping.
func TestLockOwnLostPut(t *testing.T) {
	e := newLockEnv(t)
	e.fake.PutOnce[e.store.LockKey()] = []s3fake.Answer{{Status: 412, Store: true}}
	l := e.at(0)
	if !e.acquire(l) {
		t.Fatal("want the lock")
	}
	if strings.Contains(e.warns.String(), "skipped") {
		t.Fatal(e.warns.String())
	}
	l.Release(context.Background())
	if _, ok := e.fake.Object(e.store.LockKey()); ok {
		t.Fatal("lock not released")
	}
}
