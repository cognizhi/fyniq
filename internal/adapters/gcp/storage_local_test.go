//go:build local

package gcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
)

// fakeGCSImage is the emulator the local stack runs (deploy/local/compose.yaml).
const fakeGCSImage = "fsouza/fake-gcs-server:1.56@sha256:797ce226d62f947c009dc40246b30cfb456b8473d8241407f9d6f2c04e4d69ef"

func startFakeGCS(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{Started: true, ContainerRequest: testcontainers.ContainerRequest{
		Image: fakeGCSImage, Cmd: []string{"-scheme", "http", "-port", "4443", "-backend", "memory"},
		ExposedPorts: []string{"4443/tcp"},
		WaitingFor:   wait.ForHTTP("/storage/v1/b").WithPort("4443/tcp").WithStartupTimeout(time.Minute),
	}})
	if err != nil {
		t.Fatalf("start fake-gcs-server (Docker must run): %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	endpoint, err := c.PortEndpoint(ctx, "4443/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(endpoint+"/storage/v1/b?project=fyniq-local", "application/json", strings.NewReader(`{"name":"anchors"}`))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("create the bucket: %v %v", resp, err)
	}
	_ = resp.Body.Close()
	return endpoint
}

// The production Cloud Storage adapter against the local emulator: blobs are written, read, listed in
// order and deleted, and an anchor is written once: a second Create of the same key is ErrExists, so
// two anchor jobs cannot both write one (CTL-AUD-02; the bucket's retention lock is staging's to show).
// req: CTL-AUD-02
// req: AUD-03
// asvs: V16.4.2
func TestStorageAgainstTheEmulator(t *testing.T) {
	ctx := context.Background()
	s := NewStorage(startFakeGCS(t), "anchors", nil, nil)
	key := "audit-anchors/0192f1a0-0000-7000-8000-000000000001/00000000000000000042.json"
	if err := s.Create(ctx, key, strings.NewReader(`{"seq":42}`)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Create(ctx, key, strings.NewReader(`{"seq":42,"forged":true}`)); !errors.Is(err, ports.ErrExists) {
		t.Fatalf("a second Create of an anchor: %v; want ErrExists", err)
	}
	rc, err := s.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	if string(b) != `{"seq":42}` {
		t.Fatalf("the anchor reads %q: the second Create changed it", b)
	}
	for i := range 3 {
		if err := s.Put(ctx, fmt.Sprintf("audit-anchors/x/%d.json", 3-i), bytes.NewReader([]byte("{}"))); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := s.List(ctx, "audit-anchors/x/")
	if err != nil || strings.Join(keys, ",") != "audit-anchors/x/1.json,audit-anchors/x/2.json,audit-anchors/x/3.json" {
		t.Fatalf("List = %v, %v", keys, err)
	}
	if err := s.Delete(ctx, "audit-anchors/x/1.json"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "audit-anchors/x/1.json"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("a deleted blob: %v", err)
	}
	if err := s.Put(ctx, "../escape", strings.NewReader("x")); err == nil {
		t.Error("a key outside the store was accepted")
	}
}

// The production adapter keeps a retained object as the port says, against the emulator. The emulator
// records an object's retention and the hold's metadata but enforces neither, so this proves the adapter's
// own check. It also refuses every change to a locked retention, where Cloud Storage lets one move later, so
// the extension half of porttest.BlobRetention, and the bucket's Object Retention Lock itself, are proved
// at the cloud gate (§17.5 item 1); the request Cloud Storage receives is TestStorageSendsLockedRetention….
// req: FR-DOC-02
// req: NFR-PORT-04
// asvs: V14.2.7
func TestStorageRetentionAgainstTheEmulator(t *testing.T) {
	ctx := context.Background()
	s := NewStorage(startFakeGCS(t), "anchors", nil, nil)
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	c := clock.NewFrozen(now)
	s.Clock = c
	key := "evidence/org/1/receipt.pdf"
	if err := s.SetRetainUntil(ctx, key, now.Add(time.Hour)); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("SetRetainUntil of a missing object: %v", err)
	}
	if err := s.Create(ctx, key, strings.NewReader("%PDF-1.7 receipt")); err != nil {
		t.Fatal(err)
	}
	until := now.AddDate(7, 0, 0)
	if err := s.SetRetainUntil(ctx, key, until); err != nil {
		t.Fatalf("SetRetainUntil: %v", err)
	}
	if err := s.Delete(ctx, key); !errors.Is(err, ports.ErrRetained) {
		t.Fatalf("Delete of a retained object: %v", err)
	}
	if err := s.Put(ctx, key, strings.NewReader("forged")); !errors.Is(err, ports.ErrRetained) {
		t.Fatalf("Put over a retained object: %v", err)
	}
	if err := s.SetRetainUntil(ctx, key, until.AddDate(-1, 0, 0)); !errors.Is(err, ports.ErrRetentionShortened) {
		t.Fatalf("SetRetainUntil earlier: %v", err)
	}
	if err := s.SetRetainUntil(ctx, key, until); err != nil {
		t.Fatalf("SetRetainUntil to the same time: %v", err)
	}
	if err := s.SetLegalHold(ctx, key, true); err != nil {
		t.Fatal(err)
	}
	c.Set(until.Add(time.Hour))
	if err := s.Delete(ctx, key); !errors.Is(err, ports.ErrRetained) {
		t.Fatalf("Delete of an object on hold: %v", err)
	}
	if err := s.SetLegalHold(ctx, key, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete after the retain-until with no hold: %v", err)
	}
}
