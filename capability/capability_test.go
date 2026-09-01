package capability

import (
	"encoding/json"
	"testing"

	"github.com/domainry/domainry-foundation/modulecapability"
	"github.com/domainry/domainry-foundation/modulecapability/contracttest"
	"github.com/domainry/domainry-notification-sdk/contract"
)

func TestOpenIsTopologyNeutralAndOwnerValidated(t *testing.T) {
	first, err := Open(Inputs{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(Inputs{})
	if err != nil {
		t.Fatal(err)
	}
	contracttest.VerifyBinding(t, first)
	firstSummary, err := first.CapabilitySummary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	secondSummary, err := second.CapabilitySummary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if firstSummary.Identity.ContractSHA256 != secondSummary.Identity.ContractSHA256 {
		t.Fatalf("Notification digest changed across explicit opens: %q != %q", firstSummary.Identity.ContractSHA256, secondSummary.Identity.ContractSHA256)
	}
	if !contains(firstSummary.Scenarios.ProvidedCapabilities, "notification.provider.whatsapp.meta_cloud_api") {
		t.Fatalf("official provider catalog is missing: %v", firstSummary.Scenarios.ProvidedCapabilities)
	}
	candidate, _ := json.Marshal(contract.NotificationEventType{
		Key: "ticket.assigned", Source: "project", Category: "business", DefaultSeverity: "info", Surfaces: []string{"business_workspace"}, MandatoryInApp: true,
		TemplateKey: "ticket_assigned", DefaultLocale: "en_US", Locales: map[string]contract.NotificationInboxEventTypeContent{"en_US": {Title: "Assigned", Body: "Assigned"}}, Version: 1, Status: "draft",
	})
	request := modulecapability.ValidationRequest{
		ContractVersion: modulecapability.ValidationContractVersion,
		ModuleKey:       "notification",
		CategoryKey:     "notification.delivery_governance",
		ContractSHA256:  firstSummary.Identity.ContractSHA256,
		Kind:            "notification.event_type",
		Candidate:       modulecapability.AuthoringFragment{Collection: "notification_event_types", Key: "ticket.assigned", Value: candidate},
	}
	result, err := first.ValidateCapabilityCandidate(t.Context(), request)
	if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].RuleKey != "notification.event_type.invalid" {
		t.Fatalf("event type diagnostics=%+v err=%v", result.Diagnostics, err)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
