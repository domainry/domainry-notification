package module

import (
	"errors"
	"testing"

	notificationsdk "github.com/domainry/domainry-notification-sdk"
	"github.com/domainry/domainry-notification-sdk/contract"
)

func TestPublicInboxBindingPreservesMissingAndForeignItemErrors(t *testing.T) {
	host := newIntegrationHost(t)
	profile := host.identity.profiles["full-token"]
	profile.actions = append(append([]string(nil), profile.actions...), "notification.inbox.item.get", "notification.inbox.item.mark_read")
	host.identity.profiles["full-token"] = profile
	profile.userID = "user-b"
	host.identity.profiles["inbox-other"] = profile
	binding, err := NewFactory(Options{}).OpenModule(t.Context(), integrationApplication(), host)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = binding.Publisher().PublishIntent(t.Context(), integrationIntent()); err != nil {
		t.Fatal(err)
	}
	workers, _ := binding.LocalWorkers()
	if _, err = workers.ProcessDueInboxEvents(t.Context(), 10); err != nil {
		t.Fatal(err)
	}
	owner := notificationsdk.UserAuthority{AccessToken: "full-token"}
	other := notificationsdk.UserAuthority{AccessToken: "inbox-other"}
	query := contract.NotificationInboxQuery{Scope: contract.NotificationInboxScopeMine}
	page, err := binding.Inbox().List(t.Context(), owner, query, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("owner page = %+v, error = %v", page, err)
	}
	id := page.Items[0].ID
	for _, target := range []struct {
		name      string
		authority notificationsdk.UserAuthority
		id        string
	}{
		{name: "foreign", authority: other, id: id},
		{name: "missing", authority: owner, id: "missing-item"},
	} {
		for _, operation := range []string{"get", "mark_read", "resolve"} {
			t.Run(target.name+"/"+operation, func(t *testing.T) {
				var err error
				switch operation {
				case "get":
					_, err = binding.Inbox().Get(t.Context(), target.authority, target.id, query)
				case "mark_read":
					_, err = binding.Inbox().SetRead(t.Context(), target.authority, target.id, true)
				case "resolve":
					_, err = binding.Inbox().ResolveAction(t.Context(), target.authority, target.id, "open", query)
				}
				var sdkError *notificationsdk.Error
				if !errors.As(err, &sdkError) || sdkError.StatusCode != 404 || sdkError.Code != "backend.notification.inbox_item_not_found" {
					t.Fatalf("public error = %T %v; want SDK 404 inbox_item_not_found", err, err)
				}
			})
		}
	}
	item, err := binding.Inbox().Get(t.Context(), owner, id, query)
	if err != nil || item.ReadAt != "" {
		t.Fatalf("foreign write changed owner's unread item: %+v, error = %v", item, err)
	}
	item, err = binding.Inbox().SetRead(t.Context(), owner, id, true)
	if err != nil || item.ReadAt == "" {
		t.Fatalf("owner cannot mark item read: %+v, error = %v", item, err)
	}
}
