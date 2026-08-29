package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestNotificationBusinessPersistenceStaysClassifiedAndStructured(t *testing.T) {
	root := persistenceRoot(t)
	databaseRoot := filepath.Join(root, "database")
	entries, err := os.ReadDir(databaseRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			t.Errorf("Notification database root contains unclassified source %s", entry.Name())
		}
	}
	for _, owner := range []string{"delivery", "event", "inbox", "lifecycle", "template"} {
		assertStructuredOwner(t, filepath.Join(databaseRoot, owner))
	}
}

func assertStructuredOwner(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lower := strings.ToLower(string(raw))
		for _, forbidden := range []string{"driver ==", "dialect ==", "switch driver", "switch dialect", ".offset("} {
			if strings.Contains(lower, forbidden) {
				t.Errorf("%s contains forbidden persistence pattern %q", path, forbidden)
			}
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, raw, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}
			upper := strings.ToUpper(strings.TrimSpace(value))
			if (strings.HasPrefix(upper, "SELECT ") && strings.Contains(upper, " FROM ")) || strings.HasPrefix(upper, "INSERT INTO ") || (strings.HasPrefix(upper, "UPDATE ") && strings.Contains(upper, " SET ")) || strings.HasPrefix(upper, "DELETE FROM ") {
				t.Errorf("%s contains hand-written business SQL", path)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func persistenceRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve Notification persistence root")
	}
	return filepath.Dir(filepath.Dir(source))
}
