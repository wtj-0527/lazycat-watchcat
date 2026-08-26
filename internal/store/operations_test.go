package store

import (
	"context"
	"testing"
)

func TestOperationalSettingsCollectionIntervals(t *testing.T) {
	settings := DefaultOperationalSettings()
	if settings.SystemIntervalSeconds != 15 ||
		settings.RuntimeIntervalSeconds != 30 ||
		settings.StorageIntervalSeconds != 120 ||
		settings.AdvancedIntervalSeconds != 600 {
		t.Fatalf("unexpected collection defaults: %+v", settings)
	}

	st, err := Open(t.TempDir() + "/watchcat.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.SetOperationalSettings(context.Background(), settings); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	for name, mutate := range map[string]func(*OperationalSettings){
		"system too fast":   func(value *OperationalSettings) { value.SystemIntervalSeconds = 9 },
		"runtime too slow":  func(value *OperationalSettings) { value.RuntimeIntervalSeconds = 31 },
		"storage too fast":  func(value *OperationalSettings) { value.StorageIntervalSeconds = 59 },
		"advanced too slow": func(value *OperationalSettings) { value.AdvancedIntervalSeconds = 1801 },
	} {
		t.Run(name, func(t *testing.T) {
			value := settings
			mutate(&value)
			if err := st.SetOperationalSettings(context.Background(), value); err == nil {
				t.Fatal("out-of-range interval must be rejected")
			}
		})
	}
}
