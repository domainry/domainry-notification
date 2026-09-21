package notificationhttp

import (
	"context"
	"sync"
	"testing"

	"github.com/domainry/domainry-foundation/modulecapability"
	notificationcapability "github.com/domainry/domainry-notification/capability"
)

var testCapabilityState struct {
	sync.Once
	binding modulecapability.Binding
	err     error
}

func testCapabilitySHA256(t testing.TB) string {
	t.Helper()
	binding, err := testCapabilityBinding()
	if err != nil {
		t.Fatal(err)
	}
	summary, err := binding.CapabilitySummary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return summary.Identity.ContractSHA256
}

func testCapabilityBinding() (modulecapability.Binding, error) {
	testCapabilityState.Do(func() {
		testCapabilityState.binding, testCapabilityState.err = notificationcapability.Open(notificationcapability.Inputs{})
	})
	return testCapabilityState.binding, testCapabilityState.err
}

func (*httpBindingStub) CapabilitySummary(ctx context.Context) (modulecapability.ModuleSummary, error) {
	binding, err := testCapabilityBinding()
	if err != nil {
		return modulecapability.ModuleSummary{}, err
	}
	return binding.CapabilitySummary(ctx)
}

func (*httpBindingStub) CapabilityCategory(ctx context.Context, key string) (modulecapability.CategoryDocument, error) {
	binding, err := testCapabilityBinding()
	if err != nil {
		return modulecapability.CategoryDocument{}, err
	}
	return binding.CapabilityCategory(ctx, key)
}

func (*httpBindingStub) ValidateCapabilityCandidate(ctx context.Context, request modulecapability.ValidationRequest) (modulecapability.ValidationResult, error) {
	binding, err := testCapabilityBinding()
	if err != nil {
		return modulecapability.ValidationResult{}, err
	}
	return binding.ValidateCapabilityCandidate(ctx, request)
}
