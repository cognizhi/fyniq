// Package porttest holds the conformance suites every adapter of a port must pass, local or
// cloud (NFR-PORT-04). An adapter's own test calls the suite with a factory.
package porttest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

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

// Queue checks a Queue adapter.
func Queue(t *testing.T, build func(t *testing.T) ports.Queue) {
	t.Helper()
	ctx := context.Background()
	q := build(t)
	for _, task := range []ports.Task{
		{Queue: "worker", Name: "a", Payload: []byte("1")},
		{Queue: "worker", Name: "b", Payload: []byte("2")},
		{Queue: "worker", Name: "a", Payload: []byte("duplicate")},
		{Queue: "executor", Name: "c"},
	} {
		if err := q.Enqueue(ctx, task); err != nil {
			t.Fatalf("Enqueue(%s): %v", task.Name, err)
		}
	}
	for _, want := range []string{"a", "b"} {
		got, err := q.Next(ctx, "worker")
		if err != nil || got.Name != want {
			t.Fatalf("Next(worker) = %q, %v; want %q", got.Name, err, want)
		}
		if want == "a" && string(got.Payload) != "1" {
			t.Fatalf("a duplicate name replaced the first task: %q", got.Payload)
		}
	}
	if _, err := q.Next(ctx, "worker"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("empty queue: want ErrNotFound, got %v", err)
	}
	if got, err := q.Next(ctx, "executor"); err != nil || got.Name != "c" {
		t.Fatalf("queues must be separate: %q, %v", got.Name, err)
	}
}
