package architecture

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type packageInfo struct {
	ImportPath string
	Imports    []string
}

type depRule struct {
	name       string
	source     string
	disallowed []string
}

func TestPackageDependencies(t *testing.T) {
	t.Parallel()

	pkgs := listPackages(t)
	rules := []depRule{
		{
			name:       "internal must not depend on cmd",
			source:     "copilot-proxy/internal/",
			disallowed: []string{"copilot-proxy/cmd/"},
		},
		{
			name:       "runtime config should stay infra-neutral",
			source:     "copilot-proxy/internal/runtime/config/",
			disallowed: []string{"copilot-proxy/internal/runtime/server/", "copilot-proxy/internal/runtime/api/"},
		},
		{
			name:       "runtime domain packages should not depend on app layer",
			source:     "copilot-proxy/internal/runtime/",
			disallowed: []string{"copilot-proxy/cmd/copilot-proxy/app/"},
		},
		{
			name:       "runtime api must not depend on transport middleware package",
			source:     "copilot-proxy/internal/runtime/api/",
			disallowed: []string{"copilot-proxy/internal/middleware/"},
		},
		{
			name:       "runtime endpoint must not depend on transport middleware package",
			source:     "copilot-proxy/internal/runtime/endpoint/",
			disallowed: []string{"copilot-proxy/internal/middleware/"},
		},
		{
			name:       "runtime request package must stay middleware-neutral",
			source:     "copilot-proxy/internal/runtime/request/",
			disallowed: []string{"copilot-proxy/internal/middleware/"},
		},
		{
			name:   "runtime protocol package must not depend on runtime config",
			source: "copilot-proxy/internal/runtime/protocol/",
			disallowed: []string{
				"copilot-proxy/internal/runtime/config/",
				"copilot-proxy/internal/runtime/endpoint/",
				"copilot-proxy/internal/runtime/request/",
				"copilot-proxy/internal/middleware/",
			},
		},
	}

	violations := checkRules(pkgs, rules)
	if len(violations) == 0 {
		return
	}

	t.Fatalf("dependency boundary violations:\n%s", strings.Join(violations, "\n"))
}

func checkRules(pkgs []packageInfo, rules []depRule) []string {
	var violations []string
	for _, pkg := range pkgs {
		for _, rule := range rules {
			if !strings.HasPrefix(pkg.ImportPath, rule.source) {
				continue
			}
			for _, imp := range pkg.Imports {
				for _, prefix := range rule.disallowed {
					if strings.HasPrefix(imp, prefix) {
						violations = append(violations, fmt.Sprintf("[%s] %s imports %s", rule.name, pkg.ImportPath, imp))
					}
				}
			}
		}
	}
	return violations
}

func listPackages(t *testing.T) []packageInfo {
	t.Helper()

	root := moduleRoot(t)
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list packages: %v", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(out)))
	pkgs := make([]packageInfo, 0, 64)
	for {
		var pkg packageInfo
		if err := dec.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode package list: %v", err)
		}
		if strings.TrimSpace(pkg.ImportPath) == "" {
			continue
		}
		pkgs = append(pkgs, pkg)
	}

	return pkgs
}

func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("module root not found from %s", dir)
		}
		dir = parent
	}
}
