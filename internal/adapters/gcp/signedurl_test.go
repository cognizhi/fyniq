package gcp

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// The V4 signing of a download link reproduces Google's conformance vectors (github.com/googleapis/conformance-tests,
// storage/v1/v4_signatures.json: "Simple GET", "Forward Slashes should not be stripped", "Query Parameter Ordering" and
// "Query Parameter Encoding"): the canonical request and the string to sign, byte for byte.
// req: SEC-24
// asvs: V5.3.2
func TestV4SigningMatchesGooglesVectors(t *testing.T) {
	at := time.Date(2019, 2, 1, 9, 0, 0, 0, time.UTC)
	const email = "test-iam-credentials@dummy-project-id.iam.gserviceaccount.com"
	const head = "GET\n/test-bucket/test-object\nX-Goog-Algorithm=GOOG4-RSA-SHA256&X-Goog-Credential=test-iam-credentials%40dummy-project-id.iam.gserviceaccount.com%2F20190201%2Fauto%2Fstorage%2Fgoog4_request&X-Goog-Date=20190201T090000Z&X-Goog-Expires=10"
	const tail = "\nhost:storage.googleapis.com\n\nhost\nUNSIGNED-PAYLOAD"
	const scope = "GOOG4-RSA-SHA256\n20190201T090000Z\n20190201/auto/storage/goog4_request\n"
	for name, c := range map[string]struct {
		object            string
		extra             map[string]string
		canonical, digest string
	}{
		"Simple GET": {"test-object", nil, head + "&X-Goog-SignedHeaders=host" + tail,
			"00e2fb794ea93d7adb703edaebdd509821fcc7d4f1a79ac5c8d2b394df109320"},
		"Forward Slashes should not be stripped": {"/path/with/slashes/under_score/amper&sand/file.ext", nil,
			strings.Replace(head, "/test-bucket/test-object", "/test-bucket//path/with/slashes/under_score/amper%26sand/file.ext", 1) + "&X-Goog-SignedHeaders=host" + tail,
			"63c601ecd6ccfec84f1113fc906609cbdf7651395f4300cecd96ddd2c35164f8"},
		"Query Parameter Ordering": {"test-object", map[string]string{"prefix": "/foo", "X-Goog-Meta-Foo": "bar"},
			head + "&X-Goog-Meta-Foo=bar&X-Goog-SignedHeaders=host&prefix=%2Ffoo" + tail,
			"4dafe74ad142f32b7c25fc4e6b38fd3b8a6339d7f112247573fb0066f637db6c"},
		"Query Parameter Encoding": {"test-object", map[string]string{"aA0é/=%-_.~": "~ ._-%=/é0Aa"},
			head + "&X-Goog-SignedHeaders=host&aA0%C3%A9%2F%3D%25-_.~=~%20._-%25%3D%2F%C3%A90Aa" + tail,
			"448f96c23dafa8210900554e138b2b5fd55bc53ef53b8637cecc3edec45a8fcf"},
	} {
		_, canonical, toSign := v4("test-bucket", c.object, email, at, 10*time.Second, c.extra)
		if canonical != c.canonical {
			t.Errorf("%s: the canonical request\n%q\nwant\n%q", name, canonical, c.canonical)
		}
		if toSign != scope+c.digest {
			t.Errorf("%s: the string to sign %q", name, toSign)
		}
	}
}

// A download link is signed as the service's account, lives as long as asked, at most 7 days, and makes Cloud Storage
// serve the file as an attachment with its name and type.
// req: SEC-24
// asvs: V5.3.2
func TestADownloadLinkIsSignedAndSavesTheFile(t *testing.T) {
	var signed string
	s := &Storage{Bucket: "fyniq-prod-evidence", Sign: func(_ context.Context, p []byte) (string, []byte, error) {
		signed = string(p)
		return "run-api@fyniq-prod.iam.gserviceaccount.com", []byte{0xab, 0xcd}, nil
	}}
	at := time.Date(2099, 1, 5, 9, 0, 0, 0, time.UTC)
	link, err := s.DownloadURL(context.Background(), "org/ev 1/receipt.pdf", ports.Download{FileName: "Rent, Jan.pdf", MediaType: "application/pdf",
		TTL: 5 * time.Minute}, at)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "storage.googleapis.com" || u.EscapedPath() != "/fyniq-prod-evidence/org/ev%201/receipt.pdf" || q.Get("X-Goog-Expires") != "300" ||
		q.Get("X-Goog-Signature") != "abcd" || q.Get("response-content-type") != "application/pdf" ||
		q.Get("response-content-disposition") != `attachment; filename="Rent, Jan.pdf"` ||
		!strings.HasPrefix(q.Get("X-Goog-Credential"), "run-api@fyniq-prod.iam.gserviceaccount.com/20990105/") || !strings.HasPrefix(signed, "GOOG4-RSA-SHA256\n20990105T090000Z\n") {
		t.Fatalf("the link %s, signed %q", link, signed)
	}
	for _, ttl := range []time.Duration{0, 8 * 24 * time.Hour} {
		if _, err := s.DownloadURL(context.Background(), "k", ports.Download{TTL: ttl}, at); err == nil {
			t.Errorf("a link for %s was signed", ttl)
		}
	}
	if _, err := (&Storage{Bucket: "b"}).DownloadURL(context.Background(), "k", ports.Download{TTL: time.Minute}, at); err == nil {
		t.Error("a store without a signer gave a link")
	}
}
