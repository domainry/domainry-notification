package notification_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	storeschema "github.com/domainry/domainry-notification/internal/infrastructure/persistence/database/schema"
)

func TestDDDPackageBoundaries(t *testing.T) {
	for _, legacy := range []string{"delivery", "inbox", "template", "sqlstore", "server", "application", "contract", "model", "repository", "service"} {
		if _, err := os.Stat(legacy); !os.IsNotExist(err) {
			t.Errorf("legacy or horizontal top-level package %q must not exist", legacy)
		}
	}
	command := exec.Command("go", "list", "-json", "./...")
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(output)
	type goPackage struct {
		ImportPath string
		Imports    []string
	}
	for {
		var current goPackage
		if err := decoder.Decode(&current); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		for _, imported := range current.Imports {
			assertAllowedImport(t, current.ImportPath, imported)
		}
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"internal/adapter/identitysdk", "internal/adapter/notificationsdk",
		"internal/application", "internal/application/delivery", "internal/application/inbox", "internal/application/template",
		"internal/assembly/module", "internal/assembly/saas",
		"internal/domain/notification/model", "internal/domain/template/model", "internal/domain/template/repository", "internal/domain/template/service",
		"internal/domain/template/validation", "internal/domain/inbox/model", "internal/domain/inbox/repository", "internal/domain/inbox/service",
		"internal/domain/inbox/validation", "internal/domain/delivery/model", "internal/domain/delivery/policy", "internal/domain/delivery/repository", "internal/domain/delivery/service",
		"internal/infrastructure/persistence", "internal/transport/http/saas",
	} {
		if info, err := os.Stat(filepath.FromSlash(required)); err != nil || !info.IsDir() {
			t.Errorf("required DDD package directory %q is missing", required)
		}
	}
}

func TestModuleUsesTaggedDependencies(t *testing.T) {
	content, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "replace ") || strings.Contains(string(content), "../domainry-") {
		t.Fatal("Notification must consume released module tags, not local directory replacements")
	}
}

func TestWorkspaceTablesCannotUseSystemBuilders(t *testing.T) {
	workspaceTables := map[string]bool{}
	for _, table := range storeschema.SchemaOwnership() {
		workspaceTables[table.Name] = table.Scope == storeschema.WorkspaceData
	}
	files, err := filepath.Glob("internal/infrastructure/persistence/database/**/*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.ToUpper(string(source)), " OFFSET ") {
			t.Errorf("positive-offset pagination is forbidden in persistence: %s", name)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !strings.HasPrefix(selector.Sel.Name, "New") || !strings.HasSuffix(selector.Sel.Name, "Builder") || strings.Contains(selector.Sel.Name, "Workspace") {
				return true
			}
			tableLiteral, ok := call.Args[1].(*ast.BasicLit)
			if !ok || tableLiteral.Kind != token.STRING {
				return true
			}
			table, err := strconv.Unquote(tableLiteral.Value)
			if err == nil && workspaceTables[table] {
				t.Errorf("workspace table %q uses system builder %s in %s", table, selector.Sel.Name, name)
			}
			return true
		})
	}
}

func assertAllowedImport(t *testing.T, source, imported string) {
	t.Helper()
	const root = "github.com/domainry/domainry-notification/"
	if !strings.HasPrefix(source, root+"internal/") {
		return
	}
	relativeSource := strings.TrimPrefix(source, root)
	if strings.HasPrefix(relativeSource, "internal/domain/") && (strings.HasPrefix(imported, "github.com/domainry/domainry-notification-sdk") || strings.HasPrefix(imported, "github.com/domainry/domainry-identity-sdk")) {
		t.Errorf("domain SDK dependency violation: %s imports %s", source, imported)
	}
	if !strings.HasPrefix(imported, root) {
		return
	}
	relativeImport := strings.TrimPrefix(imported, root)
	forbidden := []string{}
	switch {
	case strings.HasPrefix(relativeSource, "internal/domain/"):
		forbidden = []string{"internal/application/", "internal/infrastructure/", "internal/transport/", "internal/assembly/", "module"}
	case strings.HasPrefix(relativeSource, "internal/application/"):
		forbidden = []string{"internal/infrastructure/", "internal/transport/", "internal/assembly/", "module"}
	case strings.HasPrefix(relativeSource, "internal/infrastructure/"), strings.HasPrefix(relativeSource, "internal/transport/"):
		forbidden = []string{"internal/assembly/", "module"}
	}
	for _, prefix := range forbidden {
		if relativeImport == prefix || strings.HasPrefix(relativeImport, prefix) {
			t.Errorf("DDD dependency violation: %s imports %s", source, imported)
		}
	}
}
