package notification_test

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
		"internal/application", "internal/assembly/module", "internal/assembly/saas",
		"internal/domain/notification/model", "internal/domain/template/model", "internal/domain/template/repository", "internal/domain/template/service",
		"internal/domain/template/validation", "internal/domain/inbox/model", "internal/domain/inbox/repository", "internal/domain/inbox/service",
		"internal/domain/inbox/validation", "internal/domain/delivery/model", "internal/domain/delivery/policy", "internal/domain/delivery/repository", "internal/domain/delivery/service",
		"internal/infrastructure/identity", "internal/infrastructure/persistence/sqlstore", "internal/transport/http",
	} {
		if info, err := os.Stat(filepath.FromSlash(required)); err != nil || !info.IsDir() {
			t.Errorf("required DDD package directory %q is missing", required)
		}
	}
}

func assertAllowedImport(t *testing.T, source, imported string) {
	t.Helper()
	const root = "github.com/domainry/domainry-notification/"
	if !strings.HasPrefix(source, root+"internal/") || !strings.HasPrefix(imported, root) {
		return
	}
	relativeSource := strings.TrimPrefix(source, root)
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
