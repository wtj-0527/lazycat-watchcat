package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
	"github.com/wtj-0527/lazycat-watchcat/internal/store"
)

type ioContainerIdentity struct {
	AppID         string
	AppTitle      string
	DeployID      string
	UserID        string
	ContainerID   string
	ContainerName string
}

type ioProcessSource struct {
	protocol.ProcessSample
	AppID         string `json:"appId,omitempty"`
	AppTitle      string `json:"appTitle,omitempty"`
	DeployID      string `json:"deployId,omitempty"`
	UserID        string `json:"userId,omitempty"`
	ContainerID   string `json:"containerId,omitempty"`
	ContainerName string `json:"containerName,omitempty"`
}

type ioApplicationSource struct {
	AppID        string   `json:"appId"`
	AppTitle     string   `json:"appTitle"`
	DeployID     string   `json:"deployId,omitempty"`
	UserID       string   `json:"userId,omitempty"`
	Containers   []string `json:"containers"`
	ProcessCount int      `json:"processCount"`
	ReadRate     float64  `json:"readRate"`
	WriteRate    float64  `json:"writeRate"`
}

func (s *Server) deviceIOSources(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(r.PathValue("id"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 12
	}
	if _, err := s.store.DeviceByID(r.Context(), deviceID); store.IsNotFound(err) {
		problem(w, http.StatusNotFound, "device_not_found", "设备不存在")
		return
	} else if err != nil {
		problem(w, http.StatusInternalServerError, "internal_error", "无法读取设备")
		return
	}

	const candidateLimit = 200
	readPage, err := s.store.LatestProcesses(r.Context(), deviceID, store.ProcessListOptions{Sort: "read", Order: "desc", Limit: candidateLimit})
	if err != nil {
		problem(w, http.StatusInternalServerError, "internal_error", "无法读取进程 I/O")
		return
	}
	writePage, err := s.store.LatestProcesses(r.Context(), deviceID, store.ProcessListOptions{Sort: "write", Order: "desc", Limit: candidateLimit})
	if err != nil {
		problem(w, http.StatusInternalServerError, "internal_error", "无法读取进程 I/O")
		return
	}
	metrics, err := s.store.LatestMetricsForDevice(r.Context(), deviceID)
	if err != nil {
		problem(w, http.StatusInternalServerError, "internal_error", "无法读取容器身份")
		return
	}
	applications, _ := s.store.ListRuntimeApplications(r.Context())
	containers := ioContainerIdentities(deviceID, metrics, applications)

	unique := make(map[string]protocol.ProcessSample, len(readPage.Items)+len(writePage.Items))
	for _, item := range append(readPage.Items, writePage.Items...) {
		unique[strconv.Itoa(item.PID)+"\x00"+item.StartTime] = item
	}
	processes := make([]ioProcessSource, 0, len(unique))
	for _, item := range unique {
		if item.ReadRate <= 0 && item.WriteRate <= 0 {
			continue
		}
		source := ioProcessSource{ProcessSample: item}
		if identity, ok := matchProcessContainer(item, containers); ok {
			source.AppID, source.AppTitle = identity.AppID, identity.AppTitle
			source.DeployID, source.UserID = identity.DeployID, identity.UserID
			source.ContainerID, source.ContainerName = identity.ContainerID, identity.ContainerName
		}
		processes = append(processes, source)
	}
	sort.Slice(processes, func(i, j int) bool {
		left := processes[i].ReadRate + processes[i].WriteRate
		right := processes[j].ReadRate + processes[j].WriteRate
		if left != right {
			return left > right
		}
		return processes[i].PID < processes[j].PID
	})
	allProcesses := processes

	type applicationAccumulator struct {
		item       ioApplicationSource
		containers map[string]struct{}
		processes  map[string]struct{}
	}
	applicationMap := map[string]*applicationAccumulator{}
	for _, process := range allProcesses {
		if process.AppID == "" {
			continue
		}
		key := process.AppID + "\x00" + process.DeployID + "\x00" + process.UserID
		accumulator := applicationMap[key]
		if accumulator == nil {
			accumulator = &applicationAccumulator{
				item: ioApplicationSource{
					AppID: process.AppID, AppTitle: process.AppTitle,
					DeployID: process.DeployID, UserID: process.UserID,
				},
				containers: map[string]struct{}{}, processes: map[string]struct{}{},
			}
			applicationMap[key] = accumulator
		}
		accumulator.item.ReadRate += process.ReadRate
		accumulator.item.WriteRate += process.WriteRate
		if process.ContainerName != "" {
			accumulator.containers[process.ContainerName] = struct{}{}
		}
		accumulator.processes[strconv.Itoa(process.PID)+"\x00"+process.StartTime] = struct{}{}
	}
	appSources := make([]ioApplicationSource, 0, len(applicationMap))
	for _, accumulator := range applicationMap {
		for container := range accumulator.containers {
			accumulator.item.Containers = append(accumulator.item.Containers, container)
		}
		sort.Strings(accumulator.item.Containers)
		accumulator.item.ProcessCount = len(accumulator.processes)
		appSources = append(appSources, accumulator.item)
	}
	sort.Slice(appSources, func(i, j int) bool {
		return appSources[i].ReadRate+appSources[i].WriteRate > appSources[j].ReadRate+appSources[j].WriteRate
	})
	if len(appSources) > limit {
		appSources = appSources[:limit]
	}
	if len(processes) > limit {
		processes = processes[:limit]
	}

	collectedAt := readPage.CollectedAt
	if writePage.CollectedAt.After(collectedAt) {
		collectedAt = writePage.CollectedAt
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deviceId": deviceID, "collectedAt": collectedAt, "processTotal": readPage.Total,
		"processes": processes, "applications": appSources,
		"limitations": []string{
			"进程 I/O 来自 /proc/<pid>/io，包含文件系统缓存后的实际读写字节，但不能精确拆分到单块物理磁盘",
			"Btrfs、内核回写与块设备内部操作可能无法归属到普通用户态进程",
		},
	})
}

func ioContainerIdentities(deviceID string, metrics []store.LatestMetric, applications []store.RuntimeApplication) map[string]ioContainerIdentity {
	titles := map[string]string{}
	for _, app := range applications {
		if app.DeviceID != deviceID {
			continue
		}
		title := localizedAppTitle(app.AppID, app.Title)
		titles[app.AppID+"\x00"+app.DeployID] = title
		if titles[app.AppID] == "" {
			titles[app.AppID] = title
		}
	}
	out := map[string]ioContainerIdentity{}
	now := time.Now().UTC()
	for _, metric := range metrics {
		if !strings.HasPrefix(metric.Name, "container.") || now.Sub(metric.CollectedAt) > 10*time.Minute {
			continue
		}
		containerID := strings.TrimSpace(metric.Labels["container"])
		if containerID == "" {
			continue
		}
		appID, deployID := metric.Labels["app"], metric.Labels["deployId"]
		title := titles[appID+"\x00"+deployID]
		if title == "" {
			title = titles[appID]
		}
		if title == "" {
			title = appID
		}
		name := metric.Labels["name"]
		if name == "" {
			name = metric.Labels["service"]
		}
		if name == "" {
			name = containerID
		}
		out[containerID] = ioContainerIdentity{
			AppID: appID, AppTitle: title, DeployID: deployID, UserID: metric.Labels["userId"],
			ContainerID: containerID, ContainerName: name,
		}
	}
	return out
}

func matchProcessContainer(process protocol.ProcessSample, containers map[string]ioContainerIdentity) (ioContainerIdentity, bool) {
	haystack := process.Cgroup + " " + process.Command
	for containerID, identity := range containers {
		if len(containerID) >= 12 && strings.Contains(haystack, containerID) {
			return identity, true
		}
	}
	return ioContainerIdentity{}, false
}
