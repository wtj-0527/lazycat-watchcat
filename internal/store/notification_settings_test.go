package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestNotificationRecipientsDefaultToAdminsAndSupportExplicitUsers(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "notification-recipients.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	deviceID, err := st.EnsureLocalDevice(ctx, "nasw", "nasw", "linux/amd64", "1.0.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	users := []RuntimeUser{
		{UserID: "admin", Nickname: "Admin", Role: "admin", Online: true, Devices: []RuntimeUserDevice{{ID: "admin-client", DeviceAPIURL: "https://admin.example", Online: true}}},
		{UserID: "member", Nickname: "Member", Role: "normal", Online: true, Devices: []RuntimeUserDevice{{ID: "member-client", DeviceAPIURL: "https://member.example", Online: true}}},
	}
	if err = st.ObserveRuntimeUsers(ctx, deviceID, users); err != nil {
		t.Fatal(err)
	}

	recipients, err := st.SelectedNotificationRecipients(ctx, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recipients) != 1 || recipients[0].UserID != "admin" {
		t.Fatalf("default recipients=%+v", recipients)
	}

	settings := st.NotificationSettings(ctx)
	settings.RecipientMode = "selected"
	settings.RecipientKeys = []string{notificationRecipientKey(deviceID, "member")}
	if err = st.SetNotificationSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	recipients, err = st.SelectedNotificationRecipients(ctx, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recipients) != 1 || recipients[0].UserID != "member" {
		t.Fatalf("selected recipients=%+v", recipients)
	}
}

func TestSelectedNotificationRecipientsCannotBeEmpty(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "notification-empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	settings := DefaultNotificationSettings()
	settings.RecipientMode = "selected"
	if err = st.SetNotificationSettings(context.Background(), settings); err == nil {
		t.Fatal("expected empty selected recipient list to be rejected")
	}
}
