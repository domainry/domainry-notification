package template_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	apptemplate "github.com/domainry/domainry-notification/internal/application/template"
	notification "github.com/domainry/domainry-notification/internal/domain/notification/model"
	"github.com/domainry/domainry-notification/internal/domain/template/service"
)

type managementStore struct {
	records  map[string]template.Record
	versions map[string]map[int]template.Version
	locked   map[string]bool
	revision string
	requests map[string]template.PublicationRequest
}

func newManagementStore() *managementStore {
	return &managementStore{records: map[string]template.Record{}, versions: map[string]map[int]template.Version{}, locked: map[string]bool{}, requests: map[string]template.PublicationRequest{}}
}
func (s *managementStore) SyncPublished(context.Context, []template.Template) error { return nil }
func (s *managementStore) List(context.Context) ([]template.Record, error) {
	values := make([]template.Record, 0, len(s.records))
	for _, value := range s.records {
		values = append(values, value)
	}
	return values, nil
}
func (s *managementStore) Get(_ context.Context, key string) (template.Record, bool, error) {
	value, found := s.records[key]
	return value, found, nil
}
func (s *managementStore) ListVersions(_ context.Context, key string) ([]template.Version, error) {
	values := []template.Version{}
	for _, value := range s.versions[key] {
		values = append(values, value)
	}
	return values, nil
}
func (s *managementStore) GetVersion(_ context.Context, key string, version int) (template.Version, bool, error) {
	value, found := s.versions[key][version]
	return value, found, nil
}
func (s *managementStore) SaveDraft(_ context.Context, value template.Template, expected, actor string) (template.Record, error) {
	record, found := s.records[value.Key]
	if found && expected != "" && expected != record.UpdatedAt {
		return template.Record{}, template.ErrRecordConflict
	}
	record.Key, record.Draft, record.Status, record.UpdatedBy = value.Key, &value, "active", actor
	if record.CreatedAt == "" {
		record.CreatedAt = "created"
	}
	record.UpdatedAt = fmt.Sprintf("revision-%d", len(s.records)+len(s.versions[value.Key])+1)
	s.records[value.Key] = record
	return record, nil
}
func (s *managementStore) Publish(_ context.Context, value template.Template, expected, actor string) (template.Record, error) {
	record, found := s.records[value.Key]
	if !found || expected != "" && expected != record.UpdatedAt {
		return template.Record{}, template.ErrRecordConflict
	}
	record.Draft, record.Published, record.PublishedVersion, record.Status, record.UpdatedBy = nil, &value, value.Version, "active", actor
	record.UpdatedAt = "published"
	s.records[value.Key] = record
	if s.versions[value.Key] == nil {
		s.versions[value.Key] = map[int]template.Version{}
	}
	s.versions[value.Key][value.Version] = template.Version{TemplateKey: value.Key, Version: value.Version, Template: value, ContentHash: value.ContentHash}
	s.revision = record.UpdatedAt
	return record, nil
}
func (s *managementStore) Disable(_ context.Context, key, expected, actor string) (template.Record, error) {
	record, found := s.records[key]
	if !found {
		return template.Record{}, template.ErrRecordNotFound
	}
	if expected != "" && expected != record.UpdatedAt {
		return template.Record{}, template.ErrRecordConflict
	}
	record.Status, record.UpdatedBy, record.UpdatedAt = "disabled", actor, "disabled"
	s.records[key] = record
	s.revision = record.UpdatedAt
	return record, nil
}
func (s *managementStore) PublishedRevision(context.Context) (string, error) { return s.revision, nil }
func (s *managementStore) HasOpenPublicationRequest(_ context.Context, key string) (bool, error) {
	return s.locked[key], nil
}
func (s *managementStore) ListPublicationRequests(_ context.Context, key string) ([]template.PublicationRequest, error) {
	values := []template.PublicationRequest{}
	for _, value := range s.requests {
		if key == "" || value.TemplateKey == key {
			values = append(values, value)
		}
	}
	return values, nil
}
func (s *managementStore) GetPublicationRequest(_ context.Context, id string) (template.PublicationRequest, bool, error) {
	value, found := s.requests[id]
	return value, found, nil
}
func (s *managementStore) CreatePublicationRequest(_ context.Context, value template.PublicationRequest) error {
	if s.locked[value.TemplateKey] {
		return template.ErrPublicationConflict
	}
	s.locked[value.TemplateKey], s.requests[value.ID] = true, value
	return nil
}
func (s *managementStore) TransitionPublicationRequest(_ context.Context, id string, expected template.PublicationStatus, transition template.PublicationTransition) (template.PublicationRequest, error) {
	value, found := s.requests[id]
	if !found || value.Status != expected {
		return template.PublicationRequest{}, template.ErrPublicationConflict
	}
	if transition.ExpectedLeaseOwner != "" && (value.LeaseOwner != transition.ExpectedLeaseOwner || value.FencingToken != transition.ExpectedFencingToken) {
		return template.PublicationRequest{}, template.ErrPublicationConflict
	}
	value.Status, value.ScheduledFor, value.ReviewedBy, value.ReviewedAt = transition.Status, transition.ScheduledFor, transition.ReviewedBy, transition.ReviewedAt
	value.PublishedVersion, value.Failure, value.UpdatedAt = transition.PublishedVersion, transition.Failure, transition.UpdatedAt
	value.LeaseOwner, value.LeaseExpiresAt = "", ""
	s.requests[id] = value
	if transition.Status != template.PublicationPending && transition.Status != template.PublicationScheduled && transition.Status != template.PublicationPublishing {
		s.locked[value.TemplateKey] = false
	}
	return value, nil
}
func (s *managementStore) ListDuePublicationRequests(_ context.Context, now, _ string, limit int) ([]template.PublicationRequest, error) {
	values := []template.PublicationRequest{}
	for _, value := range s.requests {
		if value.Status == template.PublicationScheduled && value.ScheduledFor <= now {
			values = append(values, value)
		}
		if len(values) == limit {
			break
		}
	}
	return values, nil
}
func (s *managementStore) ClaimPublicationRequest(_ context.Context, id, owner, now, expires string) (template.PublicationRequest, bool, error) {
	value, found := s.requests[id]
	if !found || value.Status != template.PublicationScheduled || value.ScheduledFor > now {
		return template.PublicationRequest{}, false, nil
	}
	value.Status, value.LeaseOwner, value.LeaseExpiresAt, value.FencingToken = template.PublicationPublishing, owner, expires, value.FencingToken+1
	s.requests[id] = value
	return value, true, nil
}

func TestManagerDraftPublishDisableAndCatalogReconciliation(t *testing.T) {
	validator := emailValidator(t)
	engine, err := template.NewEngine("en-US", nil, validator, recipientDirectory{"user-1": {ID: "user-1", Email: "user@example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	store := newManagementStore()
	manager, err := template.NewManager(template.ManagerDependencies{Store: store, Engine: engine, Validator: validator})
	if err != nil {
		t.Fatal(err)
	}
	draft := validEmailTemplate()
	draft.Status, draft.Version = "draft", 1
	record, err := manager.SaveDraft(t.Context(), draft.Key, draft, "", "admin-1")
	if err != nil || record.Draft == nil {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	published, err := manager.Publish(t.Context(), draft.Key, record.UpdatedAt, "admin-1")
	if err != nil || published.Published == nil || published.Published.ContentHash == "" {
		t.Fatalf("published=%+v err=%v", published, err)
	}
	if _, err := engine.Render(t.Context(), template.RenderRequest{WorkspaceID: "workspace-1", TemplateKey: draft.Key, Recipients: []notification.UserID{"user-1"}, Variables: map[string]any{"task": "task-1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Disable(t.Context(), draft.Key, published.UpdatedAt, "admin-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Render(t.Context(), template.RenderRequest{WorkspaceID: "workspace-1", TemplateKey: draft.Key}); notification.ErrorCode(err) != "backend.notification.template_not_found" {
		t.Fatalf("err=%v", err)
	}
}

func TestManagerRejectsDraftMutationWhilePublicationIsOpen(t *testing.T) {
	validator := emailValidator(t)
	engine, _ := template.NewEngine("en-US", nil, validator, nil)
	store := newManagementStore()
	store.locked["workflow.task.opened"] = true
	manager, _ := template.NewManager(template.ManagerDependencies{Store: store, Engine: engine, Validator: validator})
	value := validEmailTemplate()
	value.Status = "draft"
	_, err := manager.SaveDraft(t.Context(), value.Key, value, "", "admin-1")
	if notification.ErrorCode(err) != "backend.notification.template_publication_locked" {
		t.Fatalf("err=%v", err)
	}
}

type publicationClock struct{ now time.Time }

func (c publicationClock) Now() time.Time { return c.now }

type workNotifier struct{ work []notification.Work }

func (n *workNotifier) Notify(_ context.Context, work notification.Work) {
	n.work = append(n.work, work)
}

func TestPublicationProcessorApprovesClaimsAndPublishesSnapshot(t *testing.T) {
	validator := emailValidator(t)
	engine, _ := template.NewEngine("en-US", nil, validator, nil)
	store := newManagementStore()
	manager, _ := template.NewManager(template.ManagerDependencies{Store: store, Engine: engine, Validator: validator})
	draft := validEmailTemplate()
	draft.Status = "draft"
	record, err := manager.SaveDraft(t.Context(), draft.Key, draft, "", "requester")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 24, 1, 0, 0, 0, time.UTC)
	notifier := &workNotifier{}
	processor, err := apptemplate.NewPublicationProcessor(apptemplate.PublicationProcessorDependencies{
		Store: store, Manager: manager, Clock: publicationClock{now: now}, WorkerID: "worker-1", WorkNotifier: notifier,
		NewRequestID:     func() (string, error) { return "request-1", nil },
		AuthorizeResumed: func(context.Context, string) (bool, error) { return true, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := processor.Request(t.Context(), draft.Key, "", record.UpdatedAt, "requester")
	if err != nil || request.Status != template.PublicationPending {
		t.Fatalf("request=%+v err=%v", request, err)
	}
	if _, err := processor.Approve(t.Context(), request.ID, "reviewer"); err != nil {
		t.Fatal(err)
	}
	if len(notifier.work) != 1 || notifier.work[0].Kind != notification.WorkPublication {
		t.Fatalf("work=%+v", notifier.work)
	}
	processed, err := processor.Process(t.Context(), request.ID)
	if err != nil || !processed {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
	finished, found, err := store.GetPublicationRequest(t.Context(), request.ID)
	if err != nil || !found || finished.Status != template.PublicationPublished || finished.PublishedVersion != 1 || store.locked[draft.Key] {
		t.Fatalf("finished=%+v found=%v locked=%v err=%v", finished, found, store.locked[draft.Key], err)
	}
}

func TestPublicationProcessorRejectsSelfApprovalAndSupersedesChangedCandidate(t *testing.T) {
	validator := emailValidator(t)
	engine, _ := template.NewEngine("en-US", nil, validator, nil)
	store := newManagementStore()
	manager, _ := template.NewManager(template.ManagerDependencies{Store: store, Engine: engine, Validator: validator})
	draft := validEmailTemplate()
	draft.Status = "draft"
	record, _ := manager.SaveDraft(t.Context(), draft.Key, draft, "", "requester")
	processor, _ := apptemplate.NewPublicationProcessor(apptemplate.PublicationProcessorDependencies{
		Store: store, Manager: manager, Clock: publicationClock{now: time.Now()}, WorkerID: "worker-1",
		NewRequestID:     func() (string, error) { return "request-1", nil },
		AuthorizeResumed: func(context.Context, string) (bool, error) { return true, nil },
	})
	request, _ := processor.Request(t.Context(), draft.Key, "", record.UpdatedAt, "requester")
	if _, err := processor.Approve(t.Context(), request.ID, "requester"); notification.ErrorCode(err) != "backend.notification.publication_self_approval_forbidden" {
		t.Fatalf("err=%v", err)
	}
	approved, err := processor.Approve(t.Context(), request.ID, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	changed := *store.records[draft.Key].Draft
	changed.Name = "Changed"
	changedRecord := store.records[draft.Key]
	changedRecord.Draft, changedRecord.UpdatedAt = &changed, "changed"
	store.records[draft.Key] = changedRecord
	processed, err := processor.Process(t.Context(), approved.ID)
	if err != nil || !processed {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
	finished := store.requests[approved.ID]
	if finished.Status != template.PublicationSuperseded || !strings.Contains(finished.Failure, "candidate_changed") {
		t.Fatalf("finished=%+v", finished)
	}
}

func TestPublicationProcessorReauthorizesReviewerBeforeResumedPublish(t *testing.T) {
	tests := []struct {
		name        string
		allowed     bool
		authorErr   error
		wantStatus  template.PublicationStatus
		wantFailure string
	}{
		{name: "revoked", allowed: false, wantStatus: template.PublicationFailed, wantFailure: "backend.notification.publication_authorization_revoked"},
		{name: "identity unavailable", authorErr: errors.New("identity unavailable"), wantStatus: template.PublicationPublishing},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator := emailValidator(t)
			engine, _ := template.NewEngine("en-US", nil, validator, nil)
			store := newManagementStore()
			manager, _ := template.NewManager(template.ManagerDependencies{Store: store, Engine: engine, Validator: validator})
			draft := validEmailTemplate()
			draft.Status = "draft"
			record, _ := manager.SaveDraft(t.Context(), draft.Key, draft, "", "requester")
			seenActor := ""
			processor, err := apptemplate.NewPublicationProcessor(apptemplate.PublicationProcessorDependencies{
				Store: store, Manager: manager, Clock: publicationClock{now: time.Now()}, WorkerID: "worker-1",
				NewRequestID: func() (string, error) { return "request-1", nil },
				AuthorizeResumed: func(_ context.Context, actor string) (bool, error) {
					seenActor = actor
					return test.allowed, test.authorErr
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			request, _ := processor.Request(t.Context(), draft.Key, "", record.UpdatedAt, "requester")
			approved, _ := processor.Approve(t.Context(), request.ID, "reviewer")
			processed, err := processor.Process(t.Context(), approved.ID)
			if err != nil || !processed || seenActor != "reviewer" {
				t.Fatalf("processed=%v actor=%q err=%v", processed, seenActor, err)
			}
			finished := store.requests[approved.ID]
			if finished.Status != test.wantStatus || finished.Failure != test.wantFailure {
				t.Fatalf("finished=%+v", finished)
			}
			if stored := store.records[draft.Key]; stored.Published != nil {
				t.Fatalf("publication must not proceed without current reviewer authorization: %+v", stored)
			}
		})
	}
}
