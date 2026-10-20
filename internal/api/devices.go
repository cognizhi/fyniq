package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ your devices (FR-ADM-03, SEC-05, rev 0.45)

// Device is one of your signed-in sessions.
type Device struct {
	ID           string    `json:"id" pattern:"^[0-9a-f]{32}$" doc:"The device's ID: derived from its session, never the session itself." example:"3f0c9a1b5d7e4c2a8b6f1e0d9c8b7a65"`
	Provider     string    `json:"provider" doc:"The sign-in provider it used." example:"google"`
	Browser      string    `json:"browser,omitempty" doc:"The browser that signed in, as it described itself; absent when it said nothing." example:"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_6) AppleWebKit/605.1.15 Safari/605.1.15"`
	SignedInAt   time.Time `json:"signed_in_at" doc:"When it signed in." example:"2026-10-20T09:00:00Z"`
	LastSeenAt   time.Time `json:"last_seen_at" doc:"When it was last used, to the minute." example:"2026-10-20T09:30:00Z"`
	ExpiresAt    time.Time `json:"expires_at" doc:"When it ends at the latest: 12 hours after it signed in (SEC-05)." example:"2026-10-20T21:00:00Z"`
	StepUpMethod string    `json:"step_up_method,omitempty" enum:"sign_in,passkey" doc:"How it last passed a second factor." example:"sign_in"`
	Current      bool      `json:"current" doc:"true for the session making this request." example:"true"`
}

// DevicePage is a page of your devices, newest first.
type DevicePage struct {
	Items      []Device `json:"items" doc:"Your signed-in sessions on this page."`
	NextCursor string   `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjNmMGM5YTFiIn0"`
}

// DevicesEnded says how many sessions were ended.
type DevicesEnded struct {
	Ended int `json:"ended" doc:"How many of your other sessions were signed out." example:"2"`
}

type deviceListInput struct{ PageQuery }

type devicePath struct {
	DeviceID string `path:"device_id" pattern:"^[0-9a-f]{32}$" doc:"The device's ID." example:"3f0c9a1b5d7e4c2a8b6f1e0d9c8b7a65"`
}

type deviceListOutput struct{ Body DevicePage }

type deviceOutput struct{ Body Device }

type devicesEndedOutput struct{ Body DevicesEnded }

// deviceID is the public ID of a session: a hash of its token's hash, so nothing shown can be used as the session.
func deviceID(tokenHash []byte) string {
	h := sha256.Sum256(append([]byte("fyniq-device\x00"), tokenHash...))
	return hex.EncodeToString(h[:16])
}

func deviceOf(d storage.Device, current []byte) Device {
	return Device{ID: deviceID(d.TokenHash), Provider: d.Provider, Browser: d.UserAgent, SignedInAt: d.SignedInAt.UTC(),
		LastSeenAt: d.LastSeenAt.UTC(), ExpiresAt: d.ExpiresAt.UTC(), StepUpMethod: d.StepUpMethod, Current: bytes.Equal(d.TokenHash, current)}
}

// devicePerson returns the caller when they are a person with a Fyniq user in a browser session, with their devices.
func (a *API) devicePerson(ctx context.Context, what string) (authn.Principal, []storage.Device, error) {
	p, err := signedInPerson(ctx, what)
	if err != nil {
		return p, nil, err
	}
	sessions, ok := a.deps.Installation.(storage.Sessions)
	if p.UserID == uuid.Nil || len(p.SessionHash) == 0 || !ok {
		return p, nil, problem(CodeSessionRequired, what+" needs a person with a Fyniq user, signed in to Fyniq in the browser.",
			"Sign in to Fyniq in the browser and do it there.")
	}
	ds, err := sessions.UserSessions(ctx, p.UserID, a.deps.Clock.Now(), authn.SessionIdle)
	return p, ds, err
}

// signOut ends one of the person's sessions.
func (a *API) signOut(ctx context.Context, p authn.Principal, d storage.Device) error {
	err := a.deps.Installation.(storage.Sessions).EndUserSession(ctx, p.UserID, d.TokenHash, a.deps.Clock.Now())
	if err == nil {
		a.deps.Log.Info("api.session.ended", "device", deviceID(d.TokenHash), "by", "its user", "request_id", requestID(ctx))
	}
	return err
}

func registerDevices(a *API) {
	const note = " Needs a person signed in to Fyniq in the browser; an OAuth client or the AI is refused."
	register(a, authz.ListDevices, huma.Operation{
		OperationID: "listDevices", Method: http.MethodGet, Path: "/v1/session/devices", Tags: []string{"Session"},
		Summary:     "List your devices",
		Description: "The browsers you are signed in on: your live sessions, newest first, with the one making this request marked (FR-ADM-03)." + note + " Read-only; no side effects.",
	}, []string{CodeCursor, CodeSessionRequired}, func(ctx context.Context, in *deviceListInput) (*deviceListOutput, error) {
		p, ds, err := a.devicePerson(ctx, "Listing your devices")
		if err != nil {
			return nil, err
		}
		all := []Device{}
		for _, d := range ds {
			all = append(all, deviceOf(d, p.SessionHash))
		}
		items, next, err := pageOf(all, func(d Device) string { return d.ID }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		return &deviceListOutput{Body: DevicePage{Items: items, NextCursor: next}}, nil
	})

	register(a, authz.SignOutDevice, exempt(huma.Operation{
		OperationID: "signOutDevice", Method: http.MethodDelete, Path: "/v1/session/devices/{device_id}", Tags: []string{"Session"},
		Summary:     "Sign a device out",
		Description: "Ends one of your sessions, such as on a lost phone: its next request is signed out (FR-ADM-03)." + note + " Side effects: the session ends, and the security log records it.",
	}, "Prefer", "Signing a device out only ends one of your own sessions; it is checked and done at once (FR-INT-14, rev 0.45)."),
		[]string{CodeNotFound, CodeSessionRequired}, func(ctx context.Context, in *devicePath) (*deviceOutput, error) {
			p, ds, err := a.devicePerson(ctx, "Signing a device out")
			if err != nil {
				return nil, err
			}
			for _, d := range ds {
				if deviceID(d.TokenHash) == in.DeviceID {
					if err := a.signOut(ctx, p, d); errors.Is(err, ports.ErrNotFound) {
						break
					} else if err != nil {
						return nil, err
					}
					return &deviceOutput{Body: deviceOf(d, p.SessionHash)}, nil
				}
			}
			return nil, problem(CodeNotFound, "You are signed in on no device with that ID.", "List your devices to find their IDs")
		})

	register(a, authz.SignOutOtherDevices, exempt(exempt(huma.Operation{
		OperationID: "signOutOtherDevices", Method: http.MethodPost, Path: "/v1/session/devices/sign-out-others", Tags: []string{"Session"},
		Summary:     "Sign out everywhere else",
		Description: "Ends every one of your sessions but the one making this request (FR-ADM-03)." + note + " Side effects: those sessions end, and the security log records each.",
	}, "Prefer", "Signing out everywhere else only ends your own sessions; it is checked and done at once (FR-INT-14, rev 0.45)."),
		"Idempotency-Key", "Signing out everywhere else is safe to repeat as it is: sent again, it ends any session begun since (FR-INT-08, rev 0.45)."),
		[]string{CodeSessionRequired}, func(ctx context.Context, _ *struct{}) (*devicesEndedOutput, error) {
			p, ds, err := a.devicePerson(ctx, "Signing out everywhere else")
			if err != nil {
				return nil, err
			}
			n := 0
			for _, d := range ds {
				if bytes.Equal(d.TokenHash, p.SessionHash) {
					continue
				}
				if err := a.signOut(ctx, p, d); err != nil && !errors.Is(err, ports.ErrNotFound) {
					return nil, err
				} else if err == nil {
					n++
				}
			}
			return &devicesEndedOutput{Body: DevicesEnded{Ended: n}}, nil
		})
}
