package archtest

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// Every dependency the product and its CI pin has its automated update pull requests (SEC-26): each file that pins one
// is in a directory an ecosystem of .github/dependabot.yml names, with a weekly schedule, a cooldown of at least 14
// days (a release two weeks old, ADR-T1) and a group, and no ecosystem names a directory that pins nothing. make scan
// runs govulncheck and pnpm audit, and the release workflow scans each image, writes its SBOM and attests it.
// req: SEC-26
// asvs: V15.2.1
func TestEveryDependencyHasItsUpdates(t *testing.T) {
	root := repoRoot(t)
	var cfg struct {
		Updates []struct {
			Ecosystem   string   `yaml:"package-ecosystem"`
			Directory   string   `yaml:"directory"`
			Directories []string `yaml:"directories"`
			Schedule    struct {
				Interval string `yaml:"interval"`
			} `yaml:"schedule"`
			Cooldown struct {
				DefaultDays int `yaml:"default-days"`
			} `yaml:"cooldown"`
			Groups map[string]any `yaml:"groups"`
		} `yaml:"updates"`
	}
	src, err := os.ReadFile(filepath.Join(root, ".github", "dependabot.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(src, &cfg); err != nil {
		t.Fatal(err)
	}
	// Where each file that pins dependencies sits, and the ecosystem that updates it. A GitHub Actions workflow is
	// updated from the repository's root.
	pins := map[string]string{
		"go.mod":                            "gomod",
		"pnpm-lock.yaml":                    "npm",
		"oracle/uv.lock":                    "uv",
		"deploy/docker/Dockerfile.*":        "docker",
		".devcontainer/Dockerfile":          "docker",
		"deploy/local/compose.yaml":         "docker-compose",
		".github/workflows/*.yml":           "github-actions",
		"infra/gcp/*/.terraform.lock.hcl":   "opentofu",
		"infra/gcp/*/*/.terraform.lock.hcl": "opentofu",
	}
	named := map[string][]string{} // ecosystem → the directories it names, as patterns
	for _, u := range cfg.Updates {
		dirs := u.Directories
		if u.Directory != "" {
			dirs = append(dirs, u.Directory)
		}
		switch {
		case len(dirs) == 0:
			t.Errorf("%s: names no directory", u.Ecosystem)
		case u.Schedule.Interval != "weekly":
			t.Errorf("%s: its schedule is %q; want weekly", u.Ecosystem, u.Schedule.Interval)
		case u.Cooldown.DefaultDays < 14:
			t.Errorf("%s: a cooldown of %d days; want at least 14", u.Ecosystem, u.Cooldown.DefaultDays)
		case len(u.Groups) == 0:
			t.Errorf("%s: no group, so each update would be its own pull request", u.Ecosystem)
		}
		named[u.Ecosystem] = append(named[u.Ecosystem], dirs...)
	}
	used := map[string]bool{}
	for pattern, eco := range pins {
		files, _ := filepath.Glob(filepath.Join(root, pattern))
		if len(files) == 0 {
			t.Errorf("%s matches no file: correct this test's list", pattern)
		}
		for _, f := range files {
			dir := "/"
			if eco != "github-actions" {
				rel, _ := filepath.Rel(root, filepath.Dir(f))
				dir = path.Clean("/" + filepath.ToSlash(rel))
			}
			found := false
			for _, p := range named[eco] {
				if ok, _ := path.Match(p, dir); ok {
					found, used[eco+" "+p] = true, true
				}
			}
			if !found {
				t.Errorf("%s: no %s update names %s", strings.TrimPrefix(f, root+"/"), eco, dir)
			}
		}
	}
	for eco, ps := range named {
		for _, p := range ps {
			if !used[eco+" "+p] {
				t.Errorf("%s names %s, where nothing it updates is pinned", eco, p)
			}
		}
	}

	scan, _ := os.ReadFile(filepath.Join(root, "scripts", "scan.py"))
	release, _ := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	images, _ := os.ReadFile(filepath.Join(root, "scripts", "images.py"))
	for what, ok := range map[string]bool{
		"make scan runs govulncheck":                 strings.Contains(string(scan), "govulncheck"),
		"make scan runs pnpm audit --prod":           strings.Contains(string(scan), "pnpm audit --prod"),
		"the release scans each image":               strings.Contains(string(release), "make image-scan"),
		"the release publishes through images.py":    strings.Contains(string(release), "scripts/images.py publish"),
		"publish writes each image's SBOM with syft": strings.Contains(string(images), `sh("syft", "scan"`),
		"publish attests the SBOM with cosign":       strings.Contains(string(images), `["attest", "--type", "spdxjson"`),
	} {
		if !ok {
			t.Errorf("%s: not found", what)
		}
	}
}
