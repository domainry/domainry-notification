package notification_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSaaSProductionDependencyClosureCannotAccessRuntimeOrPlane(t *testing.T) {
	command := exec.Command("go", "list", "-deps", "./internal/assembly/saas", "./cmd/notification-server")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("list Notification SaaS dependencies: %v\n%s", err, output)
	}
	dependencies := "\n" + string(output) + "\n"
	for _, forbidden := range []string{"github.com/domainry/domainry-runtime", "github.com/domainry/domainry-plane"} {
		if strings.Contains(dependencies, "\n"+forbidden) {
			t.Fatalf("Notification SaaS production dependency closure contains forbidden host dependency %q", forbidden)
		}
	}
	for _, required := range []string{"github.com/domainry/domainry-notification/internal/assembly/saas", "github.com/domainry/domainry-notification/internal/infrastructure/persistence", "github.com/domainry/domainry-identity-sdk"} {
		if !strings.Contains(dependencies, "\n"+required+"\n") {
			t.Fatalf("Notification SaaS production dependency closure is missing %q", required)
		}
	}
}
