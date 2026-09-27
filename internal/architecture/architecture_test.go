package architecture

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
)

const module = "RestoreSafe/"

// layers lists the package groups from bottom to top. A package may import
// packages of its own layer and of lower layers, never of higher ones.
var layers = [][]string{
	{"internal/fsx", "internal/buildinfo"},
	{"internal/security"},
	{"internal/config", "internal/logging"},
	{"internal/format"},
	{"internal/workflow"},
	{"internal/gui"},
	{"cmd"},
}

// workflowRuns are the workflows the frontend starts; no other workflow
// package may depend on them.
var workflowRuns = []string{
	"internal/workflow/backup",
	"internal/workflow/restore",
	"internal/workflow/verify",
	"internal/workflow/health",
}

// testSupport packages exist for tests only. They are exempt from the
// layers, but production code must not import them.
var testSupport = []string{"internal/testutil", "internal/e2e", "internal/architecture"}

// under reports whether pkg is dir or a package below it.
func under(pkg, dir string) bool {
	return pkg == dir || strings.HasPrefix(pkg, dir+"/")
}

func underAny(pkg string, dirs []string) bool {
	for _, d := range dirs {
		if under(pkg, d) {
			return true
		}
	}
	return false
}

// layerOf returns the layer index of pkg, or -1 if it is in no layer.
func layerOf(pkg string) int {
	for i, dirs := range layers {
		if underAny(pkg, dirs) {
			return i
		}
	}
	return -1
}

// goList runs go list once for all tests.
var goList = sync.OnceValues(func() ([]byte, error) {
	return exec.Command("go", "list", "-f", `{{.ImportPath}}|{{join .Imports ","}}`, module+"...").Output()
})

// moduleImports returns the production (non-test) imports within the module
// of every package, keyed by the path relative to the module root.
func moduleImports(t *testing.T) map[string][]string {
	t.Helper()
	out, err := goList()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	pkgs := make(map[string][]string)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		path, imports, _ := strings.Cut(strings.TrimSpace(line), "|")
		pkg := strings.TrimPrefix(path, module)
		pkgs[pkg] = nil
		for _, imp := range strings.Split(imports, ",") {
			if strings.HasPrefix(imp, module) {
				pkgs[pkg] = append(pkgs[pkg], strings.TrimPrefix(imp, module))
			}
		}
	}
	if len(pkgs) == 0 {
		t.Fatal("go list found no packages")
	}
	return pkgs
}

func TestEveryPackageBelongsToALayer(t *testing.T) {
	for pkg := range moduleImports(t) {
		if layerOf(pkg) < 0 && !underAny(pkg, testSupport) {
			t.Errorf("package %s is in no layer; add it to layers in this test and to the plan", pkg)
		}
	}
}

func TestImportsPointDownward(t *testing.T) {
	for pkg, imports := range moduleImports(t) {
		if underAny(pkg, testSupport) {
			continue
		}
		from := layerOf(pkg)
		for _, imp := range imports {
			if underAny(imp, testSupport) {
				t.Errorf("%s imports the test support package %s", pkg, imp)
				continue
			}
			if to := layerOf(imp); to > from {
				t.Errorf("%s imports %s, which belongs to a higher layer", pkg, imp)
			}
		}
	}
}

func TestWorkflowPackagesStayIndependent(t *testing.T) {
	for pkg, imports := range moduleImports(t) {
		if !under(pkg, "internal/workflow") {
			continue
		}
		for _, imp := range imports {
			switch {
			case underAny(imp, workflowRuns) && !under(imp, pkg):
				t.Errorf("%s imports the workflow %s; only the frontend starts workflows", pkg, imp)
			case under(pkg, "internal/workflow/interact") && under(imp, "internal/workflow") && !under(imp, "internal/workflow/interact"):
				t.Errorf("%s imports %s; the frontend contract must not depend on workflow code", pkg, imp)
			}
		}
	}
}

func TestWin32WrapperHasNoInternalImports(t *testing.T) {
	if imports := moduleImports(t)["internal/gui/win32"]; len(imports) > 0 {
		t.Errorf("internal/gui/win32 must only wrap the Windows API, but imports %v", imports)
	}
}
