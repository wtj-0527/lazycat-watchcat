package collector

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteHostProcessSnapshotReadsOnlyNumericProcEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "uptime"), []byte("1000.00 0.00\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	processDir := filepath.Join(root, "4254")
	if err := os.Mkdir(processDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stat := "4254 (rclone worker) R 1 1 1 0 0 0 0 0 0 0 200 100 0 0 20 0 8 0 50000 0 64\n"
	files := map[string]string{
		"stat":    stat,
		"status":  "Name:\trclone\nUid:\t0\t0\t0\t0\n",
		"io":      "read_bytes: 4096\nwrite_bytes: 8192\n",
		"cmdline": "rclone\x00mount\x00--token=secret-value\x00",
		"cgroup":  "0::/system.slice/rclone.service\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(processDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "passwd"), []byte("root:x:0:0:root:/root:/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "not-a-pid"), 0o700); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := WriteHostProcessSnapshot(&output, root, filepath.Join(root, "passwd")); err != nil {
		t.Fatal(err)
	}
	var sample rawProcessSample
	if err := json.NewDecoder(&output).Decode(&sample); err != nil {
		t.Fatal(err)
	}
	if sample.PID != 4254 || sample.Name != "rclone worker" || sample.User != "root" {
		t.Fatalf("identity=%+v", sample)
	}
	if sample.CPUTicks != 300 || sample.ReadBytes != 4096 || sample.WriteBytes != 8192 || sample.Threads != 8 {
		t.Fatalf("counters=%+v", sample)
	}
	if strings.Contains(sample.Command, "secret-value") || !strings.Contains(sample.Command, "[REDACTED]") {
		t.Fatalf("command was not redacted: %q", sample.Command)
	}
}

func TestSanitizeProcessCommandTruncatesAndRedactsSeparateSecret(t *testing.T) {
	command := sanitizeProcessCommand("tool --password hunter2 " + strings.Repeat("x", 600))
	if strings.Contains(command, "hunter2") || len(command) > 512 {
		t.Fatalf("unsafe command=%q len=%d", command, len(command))
	}
}
