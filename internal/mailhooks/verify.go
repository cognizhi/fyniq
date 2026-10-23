package mailhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Tolerance is how far a signed request's time may be from now: older ones may be replays.
const Tolerance = 5 * time.Minute

// ErrSignature means a request was not signed by the provider, or not lately.
var ErrSignature = errors.New("the request is not signed by the provider, or not within five minutes")

// VerifyResend checks a Resend (Svix) webhook: HMAC-SHA256 of "{svix-id}.{svix-timestamp}.{body}" under the
// base64 part of the whsec_ secret, against any v1 signature in svix-signature; the timestamp, in seconds, within
// Tolerance of now. It returns the delivery's id, svix-id, which is the same on every retry.
func VerifyResend(secret string, h http.Header, body []byte, now time.Time) (string, error) {
	id, ts, sigs := h.Get("svix-id"), h.Get("svix-timestamp"), h.Get("svix-signature")
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil || len(key) == 0 || id == "" || ts == "" || sigs == "" {
		return "", ErrSignature
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || absDuration(now.Sub(time.Unix(sec, 0))) > Tolerance {
		return "", ErrSignature
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	want := mac.Sum(nil)
	for _, s := range strings.Fields(sigs) {
		v, sig, ok := strings.Cut(s, ",")
		if !ok || v != "v1" {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(sig)
		if err == nil && hmac.Equal(got, want) {
			return id, nil
		}
	}
	return "", ErrSignature
}

// VerifyZeptoMail checks a ZeptoMail webhook: producer-signature is "ts=<ms>;s=<URL-encoded base64>;
// s-algorithm=HmacSHA256", s the HMAC-SHA256 under the Agent's authentication key of the body's one form value,
// URL-decoded; ts, in milliseconds, within Tolerance of now. It returns that value: the events as JSON.
func VerifyZeptoMail(key string, h http.Header, body []byte, now time.Time) ([]byte, error) {
	parts := map[string]string{}
	for _, p := range strings.Split(h.Get("producer-signature"), ";") {
		k, v, ok := strings.Cut(p, "=")
		if ok {
			parts[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	if key == "" || parts["s-algorithm"] != "HmacSHA256" {
		return nil, ErrSignature
	}
	ms, err := strconv.ParseInt(parts["ts"], 10, 64)
	if err != nil || absDuration(now.Sub(time.UnixMilli(ms))) > Tolerance {
		return nil, ErrSignature
	}
	raw, err := url.QueryUnescape(parts["s"])
	if err != nil {
		return nil, ErrSignature
	}
	got, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, ErrSignature
	}
	_, value, ok := strings.Cut(string(body), "=")
	if !ok {
		return nil, ErrSignature
	}
	payload, err := url.QueryUnescape(value)
	if err != nil {
		return nil, ErrSignature
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(payload))
	if !hmac.Equal(got, mac.Sum(nil)) {
		return nil, ErrSignature
	}
	return []byte(payload), nil
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
