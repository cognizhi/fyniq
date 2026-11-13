package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Security Command Center and Cloud Logging, read for the installation's posture (SEC-33, CTL-RES-04, CTL-DP-03;
// p1-security-hardening design D8), as the jobs' account: the findings of high and critical severity still active, and a
// sample of the services' log entries. [live-verify: SCC Standard tier on the project; the account's findingsViewer and
// logging viewer roles]

// SecurityCenterAPI and LoggingAPI are Google's APIs.
const (
	SecurityCenterAPI = "https://securitycenter.googleapis.com"
	LoggingAPI        = "https://logging.googleapis.com"
)

// SecurityCenter lists the findings of parent (projects/<project>).
type SecurityCenter struct {
	Endpoint string
	Parent   string
	r        rest
}

var _ ports.Posture = (*SecurityCenter)(nil)

// NewSecurityCenter returns the adapter.
func NewSecurityCenter(endpoint, parent string, tokens oauth2.TokenSource, client *http.Client) *SecurityCenter {
	return &SecurityCenter{Endpoint: endpoint, Parent: parent, r: rest{tokens: tokens, client: client}}
}

// Findings implements ports.Posture.
func (s *SecurityCenter) Findings(ctx context.Context) ([]ports.PostureFinding, error) {
	var out []ports.PostureFinding
	q := url.Values{"filter": {`state="ACTIVE" AND (severity="HIGH" OR severity="CRITICAL")`}, "pageSize": {"1000"}}
	for page := 0; page < 20; page++ {
		var res struct {
			Results []struct {
				Finding struct {
					Name         string    `json:"name"`
					Category     string    `json:"category"`
					Severity     string    `json:"severity"`
					ResourceName string    `json:"resourceName"`
					CreateTime   time.Time `json:"createTime"`
				} `json:"finding"`
			} `json:"listFindingsResults"`
			Next string `json:"nextPageToken"`
		}
		u := fmt.Sprintf("%s/v2/%s/sources/-/locations/global/findings?%s", s.Endpoint, s.Parent, q.Encode())
		if err := s.r.do(ctx, http.MethodGet, u, "", nil, &res); err != nil {
			return nil, fmt.Errorf("security command center: %w", err)
		}
		for _, x := range res.Results {
			f := x.Finding
			out = append(out, ports.PostureFinding{Name: f.Name, Severity: f.Severity, Category: f.Category, Resource: f.ResourceName,
				FirstSeen: f.CreateTime})
		}
		if res.Next == "" {
			return out, nil
		}
		q.Set("pageToken", res.Next)
	}
	return out, nil
}

// Logging reads the services' log entries of project.
type Logging struct {
	Endpoint string
	Project  string
	r        rest
}

var _ ports.LogReader = (*Logging)(nil)

// NewLogging returns the adapter.
func NewLogging(endpoint, project string, tokens oauth2.TokenSource, client *http.Client) *Logging {
	return &Logging{Endpoint: endpoint, Project: project, r: rest{tokens: tokens, client: client}}
}

// Read implements ports.LogReader: the Cloud Run services' and jobs' entries since since, newest first.
func (l *Logging) Read(ctx context.Context, since time.Time, limit int) ([]string, error) {
	body, err := jsonBody(map[string]any{
		"resourceNames": []string{"projects/" + l.Project},
		"filter":        fmt.Sprintf(`timestamp>=%q AND resource.type=("cloud_run_revision" OR "cloud_run_job")`, since.UTC().Format(time.RFC3339)),
		"orderBy":       "timestamp desc",
		"pageSize":      min(max(limit, 1), 1000),
	})
	if err != nil {
		return nil, err
	}
	var res struct {
		Entries []struct {
			Text string          `json:"textPayload"`
			JSON json.RawMessage `json:"jsonPayload"`
		} `json:"entries"`
	}
	if err := l.r.do(ctx, http.MethodPost, l.Endpoint+"/v2/entries:list", "application/json", body, &res); err != nil {
		return nil, fmt.Errorf("cloud logging: %w", err)
	}
	out := make([]string, 0, len(res.Entries))
	for _, e := range res.Entries {
		if e.Text != "" {
			out = append(out, e.Text)
		} else if len(e.JSON) > 0 {
			out = append(out, string(e.JSON))
		}
	}
	return out, nil
}
