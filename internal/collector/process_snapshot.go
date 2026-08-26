package collector

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type rawProcessSample struct {
	PID            int    `json:"pid"`
	StartTicks     uint64 `json:"startTicks"`
	CPUTicks       uint64 `json:"cpuTicks"`
	Name           string `json:"name"`
	UID            string `json:"uid"`
	User           string `json:"user"`
	Command        string `json:"command,omitempty"`
	State          string `json:"state"`
	Cgroup         string `json:"cgroup,omitempty"`
	MemoryRSSBytes uint64 `json:"memoryRssBytes"`
	ReadBytes      uint64 `json:"readBytes"`
	WriteBytes     uint64 `json:"writeBytes"`
	Threads        int    `json:"threads"`
	UptimeTicks    uint64 `json:"uptimeTicks"`
}

// WriteHostProcessSnapshot is the entry point used by the short-lived,
// read-only Docker helper. It only walks numeric directories directly below
// procRoot and never follows filesystem trees.
func WriteHostProcessSnapshot(w io.Writer, procRoot, passwdPath string) error {
	users := readPasswdUsers(passwdPath)
	uptimeTicks := uint64(0)
	if raw, err := os.ReadFile(filepath.Join(procRoot, "uptime")); err == nil {
		if value, parseErr := strconv.ParseFloat(strings.Fields(string(raw))[0], 64); parseErr == nil && value > 0 {
			uptimeTicks = uint64(value * 100)
		}
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if sample, ok := readRawProcess(filepath.Join(procRoot, entry.Name()), pid, uptimeTicks, users); ok {
			if err := encoder.Encode(sample); err != nil {
				return err
			}
		}
	}
	return nil
}

func readRawProcess(path string, pid int, uptimeTicks uint64, users map[string]string) (rawProcessSample, bool) {
	rawStat, err := os.ReadFile(filepath.Join(path, "stat"))
	if err != nil {
		return rawProcessSample{}, false
	}
	stat := string(rawStat)
	left, right := strings.IndexByte(stat, '('), strings.LastIndex(stat, ") ")
	if left < 0 || right <= left {
		return rawProcessSample{}, false
	}
	fields := strings.Fields(stat[right+2:])
	if len(fields) < 22 {
		return rawProcessSample{}, false
	}
	utime, err1 := strconv.ParseUint(fields[11], 10, 64)
	stime, err2 := strconv.ParseUint(fields[12], 10, 64)
	threads, err3 := strconv.Atoi(fields[17])
	startTicks, err4 := strconv.ParseUint(fields[19], 10, 64)
	rssPages, err5 := strconv.ParseInt(fields[21], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
		return rawProcessSample{}, false
	}
	uid := readProcessUID(filepath.Join(path, "status"))
	command := readProcessCommand(path)
	cgroup := readFirstLine(filepath.Join(path, "cgroup"), 512)
	readBytes, writeBytes := readProcessIO(filepath.Join(path, "io"))
	rss := uint64(0)
	if rssPages > 0 {
		rss = uint64(rssPages) * uint64(os.Getpagesize())
	}
	uptime := uint64(0)
	if uptimeTicks > startTicks {
		uptime = uptimeTicks - startTicks
	}
	return rawProcessSample{
		PID: pid, StartTicks: startTicks, CPUTicks: utime + stime,
		Name: strings.TrimSpace(stat[left+1 : right]), UID: uid, User: users[uid],
		Command: command, State: fields[0], Cgroup: cgroup,
		MemoryRSSBytes: rss, ReadBytes: readBytes, WriteBytes: writeBytes,
		Threads: threads, UptimeTicks: uptime,
	}, true
}

func readPasswdUsers(path string) map[string]string {
	out := map[string]string{}
	file, err := os.Open(path)
	if err != nil {
		return out
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) >= 3 {
			out[fields[2]] = fields[0]
		}
	}
	return out
}

func readProcessUID(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if fields := strings.Fields(scanner.Text()); len(fields) >= 2 && fields[0] == "Uid:" {
			return fields[1]
		}
	}
	return ""
}

func readProcessCommand(path string) string {
	if raw, err := os.ReadFile(filepath.Join(path, "cmdline")); err == nil {
		command := strings.TrimSpace(strings.ReplaceAll(string(raw), "\x00", " "))
		if command != "" {
			return sanitizeProcessCommand(command)
		}
	}
	if exe, err := os.Readlink(filepath.Join(path, "exe")); err == nil {
		return exe
	}
	return ""
}

func sanitizeProcessCommand(command string) string {
	command = strings.Join(strings.Fields(command), " ")
	fields := strings.Fields(command)
	for index := range fields {
		lower := strings.ToLower(fields[index])
		if strings.Contains(lower, "password=") || strings.Contains(lower, "passwd=") ||
			strings.Contains(lower, "token=") || strings.Contains(lower, "secret=") ||
			strings.Contains(lower, "access_key=") || strings.Contains(lower, "apikey=") ||
			strings.Contains(lower, "api-key=") {
			if split := strings.IndexByte(fields[index], '='); split >= 0 {
				fields[index] = fields[index][:split+1] + "[REDACTED]"
			}
		}
		if (lower == "--password" || lower == "--token" || lower == "--secret" || lower == "--api-key") && index+1 < len(fields) {
			fields[index+1] = "[REDACTED]"
		}
	}
	command = strings.Join(fields, " ")
	if len(command) > 512 {
		command = command[:512]
	}
	return command
}

func readFirstLine(path string, limit int) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line := strings.SplitN(string(raw), "\n", 2)[0]
	if len(line) > limit {
		line = line[:limit]
	}
	return strings.TrimSpace(line)
}

func readProcessIO(path string) (uint64, uint64) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	var readBytes, writeBytes uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var key string
		var value uint64
		if _, err := fmt.Sscan(scanner.Text(), &key, &value); err != nil {
			continue
		}
		switch key {
		case "read_bytes:":
			readBytes = value
		case "write_bytes:":
			writeBytes = value
		}
	}
	return readBytes, writeBytes
}
