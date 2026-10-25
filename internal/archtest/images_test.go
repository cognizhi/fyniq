package archtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The image budgets are the PRD's (NFR-IMG-04): deploy/docker/budgets.json equals the §12.9 table,
// and every image `make images` builds has one.
// req: NFR-IMG-04
func TestImageBudgetsAreThePRDs(t *testing.T) {
	root := repoRoot(t)
	var budgets struct {
		Images map[string]int `json:"images"`
	}
	b, err := os.ReadFile(filepath.Join(root, "deploy", "docker", "budgets.json"))
	if err == nil {
		err = json.Unmarshal(b, &budgets)
	}
	if err != nil {
		t.Fatal(err)
	}
	prd, err := os.ReadFile(filepath.Join(root, "docs", "prd", "prd-001-fyniq-platform.md"))
	if err != nil {
		t.Fatal(err)
	}
	section := string(prd)[strings.Index(string(prd), "### 12.9 "):]
	section = section[:strings.Index(section, "### 12.10 ")]
	budget := regexp.MustCompile(`≤ (\d+) MB`)
	name := regexp.MustCompile("`([a-z]+)`")
	want := map[string]int{}
	for _, line := range strings.Split(section, "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 5 {
			continue
		}
		m := budget.FindStringSubmatch(cells[len(cells)-2])
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		for _, img := range name.FindAllStringSubmatch(cells[1], -1) {
			want[img[1]] = n
		}
	}
	if len(want) < 9 {
		t.Fatalf("read %d budgets from §12.9: %v", len(want), want)
	}
	for img, n := range want {
		if budgets.Images[img] != n {
			t.Errorf("%s: budgets.json says %d MB, §12.9 says %d MB", img, budgets.Images[img], n)
		}
	}
	for img := range budgets.Images {
		if _, ok := want[img]; !ok {
			t.Errorf("budgets.json has %s, which §12.9 does not list", img)
		}
	}
	mk, _ := os.ReadFile(filepath.Join(root, "Makefile"))
	goImages := regexp.MustCompile(`(?m)^GO_IMAGES := (.+)$`).FindSubmatch(mk)
	if goImages == nil {
		t.Fatal("no GO_IMAGES in the Makefile")
	}
	for _, img := range append(strings.Fields(string(goImages[1])), "web") {
		if _, ok := budgets.Images[img]; !ok {
			t.Errorf("make images builds %s, which has no budget", img)
		}
	}
}

// Every base image is pinned by digest (NFR-IMG-03), in the Dockerfiles and in the local stack.
// req: NFR-IMG-03
func TestImagesArePinnedByDigest(t *testing.T) {
	root := repoRoot(t)
	pinned := regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)
	files, _ := filepath.Glob(filepath.Join(root, "deploy", "docker", "Dockerfile.*"))
	if len(files) < 2 {
		t.Fatalf("Dockerfiles: %v", files)
	}
	for _, f := range files {
		src, _ := os.ReadFile(f)
		var stages []string
		for _, line := range strings.Split(string(src), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || !strings.EqualFold(fields[0], "FROM") {
				continue
			}
			ref := fields[1]
			if strings.HasPrefix(ref, "--") && len(fields) > 2 {
				ref = fields[2]
			}
			if !slices.Contains(stages, ref) && !pinned.MatchString(ref) { // FROM an earlier stage needs no digest
				t.Errorf("%s: FROM %s is not pinned by digest", filepath.Base(f), ref)
			}
			if len(fields) >= 4 && strings.EqualFold(fields[len(fields)-2], "AS") {
				stages = append(stages, fields[len(fields)-1])
			}
		}
	}
	var compose struct {
		Services map[string]struct {
			Image string `yaml:"image"`
		} `yaml:"services"`
	}
	src, _ := os.ReadFile(filepath.Join(root, "deploy", "local", "compose.yaml"))
	if err := yaml.Unmarshal(src, &compose); err != nil {
		t.Fatal(err)
	}
	for name, s := range compose.Services {
		if !strings.HasPrefix(s.Image, "fyniq-") && !pinned.MatchString(s.Image) {
			t.Errorf("compose service %s: %s is not pinned by digest", name, s.Image)
		}
	}
}

// Our services run in the local stack as in production (NFR-IMG-02): read-only root, non-root
// user, no capabilities, no privilege escalation, writable paths only on tmpfs.
// req: NFR-IMG-02
// req: SEC-27
// asvs: V15.2.3
func TestComposeHardensOurServices(t *testing.T) {
	var compose struct {
		Services map[string]struct {
			Image       string   `yaml:"image"`
			ReadOnly    bool     `yaml:"read_only"`
			User        string   `yaml:"user"`
			CapDrop     []string `yaml:"cap_drop"`
			SecurityOpt []string `yaml:"security_opt"`
			Tmpfs       []string `yaml:"tmpfs"`
			Volumes     []string `yaml:"volumes"`
		} `yaml:"services"`
	}
	src, _ := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "local", "compose.yaml"))
	if err := yaml.Unmarshal(src, &compose); err != nil {
		t.Fatal(err)
	}
	ours := 0
	for name, s := range compose.Services {
		if !strings.HasPrefix(s.Image, "fyniq-") && !strings.HasPrefix(s.Image, "oryd/hydra:") { // ours, and the OAuth server
			continue
		}
		ours++
		want := "65532:65532"
		if name == "web" {
			want = "101:101" // nginx-unprivileged (§12.9)
		}
		if !s.ReadOnly || s.User != want || !slices.Equal(s.CapDrop, []string{"ALL"}) ||
			!slices.Contains(s.SecurityOpt, "no-new-privileges:true") || len(s.Tmpfs) == 0 || len(s.Volumes) > 0 {
			t.Errorf("compose service %s is not hardened: read_only %v, user %q, cap_drop %v, security_opt %v, tmpfs %v, volumes %v",
				name, s.ReadOnly, s.User, s.CapDrop, s.SecurityOpt, s.Tmpfs, s.Volumes)
		}
	}
	if ours < 10 {
		t.Fatalf("only %d of our services (and the OAuth server's) in the local stack", ours)
	}
}

// Every image builds for linux/amd64 and linux/arm64 (NFR-IMG-08): the build stage runs on the build
// machine and takes its target from BuildKit, nothing names one architecture, and CI builds on both.
// That each pinned digest is a multi-platform index needs the registries: `make images` checks it.
// req: NFR-IMG-08
func TestImagesBuildForBothPlatforms(t *testing.T) {
	root := repoRoot(t)
	oneArch := regexp.MustCompile(`GOARCH=(amd64|arm64)|--platform[= ]linux/`)
	for _, f := range []string{"deploy/docker/Dockerfile.go-service", "deploy/docker/Dockerfile.web", ".devcontainer/Dockerfile"} {
		src, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "#") && oneArch.MatchString(line) {
				t.Errorf("%s:%d names one architecture: %s", f, i+1, strings.TrimSpace(line))
			}
		}
		if f == ".devcontainer/Dockerfile" && !(strings.Contains(string(src), "TARGETARCH") &&
			strings.Contains(string(src), "linux-x64") && strings.Contains(string(src), "linux-arm64")) {
			t.Errorf("%s must install mise for its own platform, amd64 (linux-x64) or arm64 (linux-arm64), from TARGETARCH", f)
		}
		if strings.HasPrefix(f, "deploy/") && !regexp.MustCompile(`(?m)^FROM --platform=\$BUILDPLATFORM \S+ AS build$`).Match(src) {
			t.Errorf("%s: the build stage must run on the build machine: FROM --platform=$BUILDPLATFORM … AS build", f)
		}
	}
	goSvc, _ := os.ReadFile(filepath.Join(root, "deploy", "docker", "Dockerfile.go-service"))
	if !strings.Contains(string(goSvc), "GOARCH=${TARGETARCH}") || !strings.Contains(string(goSvc), "GOOS=${TARGETOS}") {
		t.Error("Dockerfile.go-service must cross-compile for BuildKit's target: GOOS=${TARGETOS} GOARCH=${TARGETARCH}")
	}
	var ci struct {
		Jobs map[string]struct {
			RunsOn string `yaml:"runs-on"`
			Steps  []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	src, _ := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err := yaml.Unmarshal(src, &ci); err != nil {
		t.Fatal(err)
	}
	built := map[string]bool{}
	for _, j := range ci.Jobs {
		for _, s := range j.Steps {
			if strings.HasPrefix(s.Run, "make images") {
				built[map[bool]string{true: "arm64", false: "amd64"}[strings.HasSuffix(j.RunsOn, "-arm")]] = true
			}
		}
	}
	if !built["amd64"] || !built["arm64"] {
		t.Errorf("CI must run `make images` on an amd64 and an arm64 runner; it runs on %v", built)
	}
}

// Every partner document in contracts/partners/ has its mock in the local stack's partners profile (§12.8,
// p1-connector-framework task 8.1): a service that runs Prism at an exact version on that document, from an image pinned by
// digest, so local mode starts it without the partner and make up starts without it.
// req: platform/local-mode Local edge, email, messaging and partner substitutes
func TestEveryPartnerDocumentIsMocked(t *testing.T) {
	root := repoRoot(t)
	docs, err := filepath.Glob(filepath.Join(root, "contracts", "partners", "*.yaml"))
	if err != nil || len(docs) == 0 {
		t.Fatalf("no partner documents: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "deploy", "local", "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Image    string   `yaml:"image"`
			Profiles []string `yaml:"profiles"`
			Command  []string `yaml:"command"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &compose); err != nil {
		t.Fatal(err)
	}
	prism := regexp.MustCompile(`^@stoplight/prism-cli@\d+\.\d+\.\d+$`)
	for _, doc := range docs {
		want := "/partners/" + filepath.Base(doc)
		found := false
		for name, s := range compose.Services {
			if !slices.Contains(s.Command, want) {
				continue
			}
			found = true
			switch {
			case !slices.Equal(s.Profiles, []string{"partners"}):
				t.Errorf("%s mocks %s outside the partners profile: %v", name, want, s.Profiles)
			case !strings.Contains(s.Image, "@sha256:"):
				t.Errorf("%s's image is not pinned by digest: %s", name, s.Image)
			case !slices.ContainsFunc(s.Command, prism.MatchString):
				t.Errorf("%s does not run Prism at an exact version: %v", name, s.Command)
			}
		}
		if !found {
			t.Errorf("contracts/partners/%s has no mock in deploy/local/compose.yaml's partners profile", filepath.Base(doc))
		}
	}
}
