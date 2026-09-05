package capability

import (
	"bytes"
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
		Key: "ticket.assigned", Source: "project", Category: "business", DefaultSeverity: "info", MandatoryInApp: true,
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
	authoringCandidate, _ := json.Marshal(contract.NotificationEventType{
		Key: "ticket.resolved", Source: "project", Category: "business", DefaultSeverity: "info", MandatoryInApp: true,
		TemplateKey: "ticket_resolved", DefaultLocale: "en_US",
		Locales: map[string]contract.NotificationInboxEventTypeContent{"en_US": {Title: "Resolved", Body: "Resolved", ActionLabels: map[string]string{"open_ticket": "Open"}}},
		Actions: []contract.NotificationInboxActionDescriptor{{Key: "open_ticket", ResourceType: "project_record", RouteKey: "ticket.detail"}},
	})
	request.Candidate = modulecapability.AuthoringFragment{Collection: "notification_event_types", Key: "ticket.resolved", Value: authoringCandidate}
	result, err = first.ValidateCapabilityCandidate(t.Context(), request)
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("authoring event type diagnostics=%+v err=%v", result.Diagnostics, err)
	}
}

func TestProjectHandlerEventRequiresPublishedProjectRecordSemanticRoute(t *testing.T) {
	binding, err := Open(Inputs{})
	if err != nil {
		t.Fatal(err)
	}
	document, err := binding.CapabilityCategory(t.Context(), "notification.delivery_governance")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Projections) != 1 || document.Projections[0].Kind != "notification.authoring_rule" || document.Projections[0].Key != "project_handler_event_semantic_route" || !contains(document.ValidationContracts[0].ReferencedCollections, "actions") {
		t.Fatalf("delivery governance authoring rule=%+v validations=%+v", document.Projections, document.ValidationContracts)
	}
	if !json.Valid(document.Projections[0].Payload) || !containsText(document.Projections[0].Payload, "route_key_inference") || !containsText(document.Projections[0].Payload, "project_record") {
		t.Fatalf("semantic route rule is not machine-readable: %s", document.Projections[0].Payload)
	}
	summary, err := binding.CapabilitySummary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	event, _ := json.Marshal(contract.NotificationEventType{
		Key: "ticket.assigned", Source: "project", Category: "business", DefaultSeverity: "info", MandatoryInApp: true,
		TemplateKey: "ticket_assigned", DefaultLocale: "en_US", Locales: map[string]contract.NotificationInboxEventTypeContent{"en_US": {Title: "Assigned", Body: "Assigned"}},
	})
	action, _ := json.Marshal(map[string]any{"key": "ticket.assign", "handler": map[string]any{"notification_event_types": []string{"ticket.assigned"}}})
	request := modulecapability.ValidationRequest{
		ContractVersion: modulecapability.ValidationContractVersion, ModuleKey: "notification", CategoryKey: "notification.delivery_governance",
		ContractSHA256: summary.Identity.ContractSHA256, Kind: "notification.event_type",
		Candidate:         modulecapability.AuthoringFragment{Collection: "notification_event_types", Key: "ticket.assigned", Value: event},
		ReferencedContext: []modulecapability.AuthoringFragment{{Collection: "actions", Key: "ticket.assign", Value: action}},
	}
	result, err := binding.ValidateCapabilityCandidate(t.Context(), request)
	if err != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].RuleKey != "notification.event_type.project_record_action_required" || result.Diagnostics[0].Params["event_type"] != "ticket.assigned" || result.Diagnostics[0].Params["action_key"] != "ticket.assign" {
		t.Fatalf("missing route diagnostics=%+v err=%v", result.Diagnostics, err)
	}

	var decoded contract.NotificationEventType
	if err := json.Unmarshal(event, &decoded); err != nil {
		t.Fatal(err)
	}
	decoded.Actions = []contract.NotificationInboxActionDescriptor{{Key: "open_ticket", ResourceType: "project_record", RouteKey: "business.tickets.detail"}}
	localized := decoded.Locales["en_US"]
	localized.ActionLabels = map[string]string{"open_ticket": "Open"}
	decoded.Locales["en_US"] = localized
	request.Candidate.Value, _ = json.Marshal(decoded)
	result, err = binding.ValidateCapabilityCandidate(t.Context(), request)
	if err != nil || len(result.Diagnostics) != 0 {
		t.Fatalf("valid semantic route rejected: diagnostics=%+v err=%v", result.Diagnostics, err)
	}
}

func containsText(value []byte, target string) bool {
	return bytes.Contains(value, []byte(target))
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
