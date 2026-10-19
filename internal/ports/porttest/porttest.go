// Package porttest holds the conformance suites every adapter of a port must pass, local or
// cloud (NFR-PORT-04). An adapter's own test calls the suite with a factory.
package porttest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Secrets checks a Secrets adapter. seed stores name=value pairs before the adapter is built.
func Secrets(t *testing.T, build func(t *testing.T, seed map[string]string) ports.Secrets) {
	t.Helper()
	ctx := context.Background()
	s := build(t, map[string]string{"SMTP_PASSWORD": "local-only-value"})
	got, err := s.Get(ctx, "SMTP_PASSWORD")
	if err != nil || got != "local-only-value" {
		t.Fatalf("Get(SMTP_PASSWORD) = %q, %v", got, err)
	}
	if _, err := s.Get(ctx, "MISSING_SECRET"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Get(MISSING_SECRET): want ErrNotFound, got %v", err)
	}
}

// BlobStore checks a BlobStore adapter.
func BlobStore(t *testing.T, build func(t *testing.T) ports.BlobStore) {
	t.Helper()
	ctx := context.Background()
	b := build(t)
	if err := b.Put(ctx, "org/1/doc.txt", bytes.NewBufferString("hello")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := b.Put(ctx, "org/1/doc.txt", bytes.NewBufferString("hello again")); err != nil {
		t.Fatalf("Put (overwrite): %v", err)
	}
	rc, err := b.Get(ctx, "org/1/doc.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	data, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(data) != "hello again" {
		t.Fatalf("Get: %q", data)
	}
	if err := b.Delete(ctx, "org/1/doc.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := b.Get(ctx, "org/1/doc.txt"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Get after Delete: want ErrNotFound, got %v", err)
	}
	if err := b.Put(ctx, "../escape.txt", bytes.NewBufferString("x")); err == nil {
		t.Fatalf("Put(../escape.txt): want an error for a key outside the store")
	}
	// Create is write-once (audit anchors).
	if err := b.Create(ctx, "anchors/org/1.json", bytes.NewBufferString("first")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := b.Create(ctx, "anchors/org/1.json", bytes.NewBufferString("second")); !errors.Is(err, ports.ErrExists) {
		t.Fatalf("Create over an existing blob: want ErrExists, got %v", err)
	}
	if err := b.Create(ctx, "anchors/org/2.json", bytes.NewBufferString("x")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := b.Put(ctx, "other/3.json", bytes.NewBufferString("x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	keys, err := b.List(ctx, "anchors/org/")
	if err != nil || len(keys) != 2 || keys[0] != "anchors/org/1.json" || keys[1] != "anchors/org/2.json" {
		t.Fatalf("List(anchors/org/) = %v, %v; want the two anchors, sorted", keys, err)
	}
	rc, err = b.Get(ctx, "anchors/org/1.json")
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(rc)
	_ = rc.Close()
	if string(data) != "first" {
		t.Fatalf("the write-once blob was overwritten: %q", data)
	}
}

// BlobRetention checks a BlobStore adapter's retention (FR-DOC-02, G4-068): a retain-until only moves later;
// while it is in the future, or the blob is on legal hold, nobody can delete or replace the blob; once it
// passes and no hold is left, the blob can go. build gets the clock the adapter must judge time by.
func BlobRetention(t *testing.T, build func(t *testing.T, c ports.Clock) ports.BlobStore) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	c := clock.NewFrozen(now)
	b := build(t, c)
	key := "evidence/org/1/receipt.pdf"
	if err := b.SetRetainUntil(ctx, key, now.Add(time.Hour)); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("SetRetainUntil of a missing blob: want ErrNotFound, got %v", err)
	}
	if err := b.SetLegalHold(ctx, key, true); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("SetLegalHold of a missing blob: want ErrNotFound, got %v", err)
	}
	if err := b.Create(ctx, key, bytes.NewBufferString("%PDF-1.7 receipt")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	until := now.AddDate(7, 0, 0)
	if err := b.SetRetainUntil(ctx, key, until); err != nil {
		t.Fatalf("SetRetainUntil: %v", err)
	}
	if err := b.Delete(ctx, key); !errors.Is(err, ports.ErrRetained) {
		t.Fatalf("Delete of a retained blob: want ErrRetained, got %v", err)
	}
	if err := b.Put(ctx, key, bytes.NewBufferString("forged")); !errors.Is(err, ports.ErrRetained) {
		t.Fatalf("Put over a retained blob: want ErrRetained, got %v", err)
	}
	if err := b.SetRetainUntil(ctx, key, until.AddDate(-1, 0, 0)); !errors.Is(err, ports.ErrRetentionShortened) {
		t.Fatalf("SetRetainUntil earlier: want ErrRetentionShortened, got %v", err)
	}
	if err := b.SetRetainUntil(ctx, key, until); err != nil {
		t.Fatalf("SetRetainUntil to the same time: %v", err)
	}
	if err := b.SetRetainUntil(ctx, key, until.AddDate(1, 0, 0)); err != nil {
		t.Fatalf("SetRetainUntil later: %v", err)
	}
	rc, err := b.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get of a retained blob: %v", err)
	}
	data, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(data) != "%PDF-1.7 receipt" {
		t.Fatalf("the retained blob changed: %q", data)
	}
	// Past the retain-until, a legal hold still keeps it; without the hold it can go.
	if err := b.SetLegalHold(ctx, key, true); err != nil {
		t.Fatalf("SetLegalHold: %v", err)
	}
	c.Set(until.AddDate(1, 0, 1))
	if err := b.Delete(ctx, key); !errors.Is(err, ports.ErrRetained) {
		t.Fatalf("Delete of a blob on legal hold: want ErrRetained, got %v", err)
	}
	if err := b.SetLegalHold(ctx, key, false); err != nil {
		t.Fatalf("SetLegalHold(false): %v", err)
	}
	if err := b.Delete(ctx, key); err != nil {
		t.Fatalf("Delete after the retain-until with no hold: %v", err)
	}
	if _, err := b.Get(ctx, key); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("Get after Delete: want ErrNotFound, got %v", err)
	}
	// A hold alone, without a retain-until, keeps a blob too.
	if err := b.Put(ctx, "staging/org/2.pdf", bytes.NewBufferString("x")); err != nil {
		t.Fatal(err)
	}
	if err := b.SetLegalHold(ctx, "staging/org/2.pdf", true); err != nil {
		t.Fatal(err)
	}
	if err := b.Delete(ctx, "staging/org/2.pdf"); !errors.Is(err, ports.ErrRetained) {
		t.Fatalf("Delete of a held blob: want ErrRetained, got %v", err)
	}
	if keys, err := b.List(ctx, ""); err != nil || len(keys) != 1 || keys[0] != "staging/org/2.pdf" {
		t.Fatalf("List = %v, %v; want only the held blob (retention records are not blobs)", keys, err)
	}
}

// eicar is the antivirus test file, split so this source file is not itself flagged.
var eicar = `X5O!P%@AP[4\PZX54(P^)7CC)7}$` + `EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`

// Scanner checks a Scanner adapter (FR-DOC-01): a clean file passes, and the EICAR test file is flagged
// alone, inside a larger file and across a read boundary.
func Scanner(t *testing.T, s ports.Scanner) {
	t.Helper()
	ctx := context.Background()
	if r, err := s.Scan(ctx, bytes.NewBufferString("%PDF-1.7 a clean receipt")); err != nil || !r.Clean {
		t.Fatalf("a clean file: %+v, %v", r, err)
	}
	big := bytes.Repeat([]byte("a"), 64<<10-10)
	for name, body := range map[string][]byte{
		"alone":           []byte(eicar),
		"inside":          append(append([]byte("prefix "), eicar...), " suffix"...),
		"across a buffer": append(append([]byte(nil), big...), eicar...),
	} {
		r, err := s.Scan(ctx, iotest(body))
		if err != nil || r.Clean || r.Signature == "" {
			t.Errorf("EICAR %s: %+v, %v; want flagged with a signature", name, r, err)
		}
	}
}

// iotest returns a reader of b that hands out at most 1,000 bytes a read, as a network stream does.
func iotest(b []byte) io.Reader { return &smallReads{b: b} }

type smallReads struct{ b []byte }

func (s *smallReads) Read(p []byte) (int, error) {
	if len(s.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p[:min(len(p), 1000)], s.b)
	s.b = s.b[n:]
	return n, nil
}

// Signer checks a Signer adapter (CTL-AUD-02).
func Signer(t *testing.T, s ports.Signer) {
	t.Helper()
	ctx := context.Background()
	msg := []byte(`{"seq":1}`)
	sig, err := s.Sign(ctx, msg)
	if err != nil || s.KeyID() == "" {
		t.Fatalf("Sign = %x, %v (key %q)", sig, err, s.KeyID())
	}
	if err := s.Verify(s.KeyID(), msg, sig); err != nil {
		t.Fatalf("Verify of a good signature: %v", err)
	}
	if err := s.Verify(s.KeyID(), []byte(`{"seq":2}`), sig); !errors.Is(err, ports.ErrBadSignature) {
		t.Fatalf("Verify of a changed message: want ErrBadSignature, got %v", err)
	}
	if err := s.Verify("another-key", msg, sig); !errors.Is(err, ports.ErrBadSignature) {
		t.Fatalf("Verify under another key: want ErrBadSignature, got %v", err)
	}
}

// Queue checks a Queue adapter (NFR-PORT-04, One outbox contract for every adapter). build returns
// the queue and a function that waits until delivered has n tasks for class, returning them.
func Queue(t *testing.T, build func(t *testing.T) (q ports.Queue, delivered func(class string, n int) []ports.Task)) {
	t.Helper()
	ctx := context.Background()
	q, delivered := build(t)
	for _, task := range []ports.Task{
		{Queue: "worker", Name: "a", Payload: []byte(`{"n":1}`)},
		{Queue: "worker", Name: "b", Payload: []byte(`{"n":2}`)},
		{Queue: "worker", Name: "a", Payload: []byte(`{"n":"duplicate"}`)},
		{Queue: "executor", Name: "c", Payload: []byte(`{}`)},
	} {
		if err := q.Enqueue(ctx, task); err != nil {
			t.Fatalf("Enqueue(%s): %v", task.Name, err)
		}
	}
	got := delivered("worker", 2)
	names := map[string]string{}
	for _, task := range got {
		names[task.Name] = string(task.Payload)
	}
	if len(got) != 2 || names["a"] != `{"n":1}` || names["b"] != `{"n":2}` {
		t.Fatalf("worker received %v; want a and b once each, a with its first payload", names)
	}
	if c := delivered("executor", 1); len(c) != 1 || c[0].Name != "c" {
		t.Fatalf("each class has its own queue: executor received %v", c)
	}
	if err := q.Enqueue(ctx, ports.Task{Queue: "nowhere", Name: "d"}); err == nil {
		t.Fatal("Enqueue to an unknown worker class succeeded")
	}
}

// TaskTokens checks a TaskTokens adapter (Proposal executor queue isolation).
func TaskTokens(t *testing.T, tokens ports.TaskTokens) {
	t.Helper()
	worker, err := tokens.Token("worker")
	if err != nil {
		t.Fatal(err)
	}
	executor, err := tokens.Token("executor")
	if err != nil {
		t.Fatal(err)
	}
	if err := tokens.Verify(worker, "worker"); err != nil {
		t.Fatalf("a worker token for the worker: %v", err)
	}
	if err := tokens.Verify(executor, "executor"); err != nil {
		t.Fatalf("an executor token for the executor: %v", err)
	}
	for name, tok := range map[string]string{"another class's token": worker, "no token": "", "a forged token": executor[:len(executor)-2] + "xx"} {
		if err := tokens.Verify(tok, "executor"); !errors.Is(err, ports.ErrUnauthorised) {
			t.Errorf("the executor accepted %s: %v", name, err)
		}
	}
}
