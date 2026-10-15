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
		if !strings.HasPrefix(s.Image, "fyniq-") {
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
	if ours < 8 {
		t.Fatalf("only %d of our services in the local stack", ours)
	}
}
