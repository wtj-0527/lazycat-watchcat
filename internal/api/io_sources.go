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
	AppID              string            `json:"appId"`
	AppTitle           string            `json:"appTitle"`
	DeployID           string            `json:"deployId,omitempty"`
	UserID             string            `json:"userId,omitempty"`
	UserName           string            `json:"userName,omitempty"`
	InstanceStatus     string            `json:"instanceStatus,omitempty"`
	Containers         []string          `json:"containers"`
	ProcessCount       int               `json:"processCount"`
	ActiveProcessCount int               `json:"activeProcessCount"`
	ReadRate           float64           `json:"readRate"`
	WriteRate          float64           `json:"writeRate"`
	Processes          []ioProcessSource `json:"processes,omitempty"`
}

func (s *Server) deviceIOSources(w http.ResponseWriter, r *http.Request) {
	deviceID := strings.TrimSpace(r.PathValue("id"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	if _, err := s.store.DeviceByID(r.Context(), deviceID); store.IsNotFound(err) {
		problem(w, http.StatusNotFound, "device_not_found", "设备不存在")
		return
	} else if err != nil {
		problem(w, http.StatusInternalServerError, "internal_error", "无法读取设备")
		return
	}

	const candidateLimit = 5000
	processPage, err := s.store.LatestProcesses(r.Context(), deviceID, store.ProcessListOptions{Sort: "io", Order: "desc", Limit: candidateLimit})
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

	allProcesses := make([]ioProcessSource, 0, len(processPage.Items))
	for _, item := range processPage.Items {
		source := ioProcessSource{ProcessSample: item}
		if identity, ok := matchProcessContainer(item, containers); ok {
			source.AppID, source.AppTitle = identity.AppID, identity.AppTitle
			source.DeployID, source.UserID = identity.DeployID, identity.UserID
			source.ContainerID, source.ContainerName = identity.ContainerID, identity.ContainerName
		}
		allProcesses = append(allProcesses, source)
	}
	type applicationAccumulator struct {
		item       ioApplicationSource
		containers map[string]struct{}
		processes  map[string]struct{}
	}
	applicationMap := map[string]*applicationAccumulator{}
	applicationKey := func(appID, deployID, userID string) string {
		return appID + "\x00" + deployID + "\x00" + userID
	}
	ensureApplication := func(appID, title, deployID, userID string) (*applicationAccumulator, string) {
		key := applicationKey(appID, deployID, userID)
		accumulator := applicationMap[key]
		if accumulator == nil {
			accumulator = &applicationAccumulator{
				item: ioApplicationSource{
					AppID: appID, AppTitle: title, DeployID: deployID, UserID: userID,
				},
				containers: map[string]struct{}{}, processes: map[string]struct{}{},
			}
			applicationMap[key] = accumulator
		}
		return accumulator, key
	}
	runtimeByDeploy := map[string]string{}
	runtimeByUser := map[string]string{}
	runtimeKeysByApp := map[string][]string{}
	for _, application := range applications {
		if application.DeviceID != deviceID || application.AppID == "" || application.DeployID == "" {
			continue
		}
		accumulator, key := ensureApplication(
			application.AppID,
			localizedAppTitle(application.AppID, application.Title),
			application.DeployID,
			application.UserID,
		)
		accumulator.item.UserName = application.UserName
		accumulator.item.InstanceStatus = application.InstanceStatus
		runtimeByDeploy[application.AppID+"\x00"+application.DeployID] = key
		if application.UserID != "" {
			runtimeByUser[application.AppID+"\x00"+application.UserID] = key
		}
		runtimeKeysByApp[application.AppID] = append(runtimeKeysByApp[application.AppID], key)
	}
	resolveApplication := func(appID, title, deployID, userID string) *applicationAccumulator {
		if accumulator := applicationMap[applicationKey(appID, deployID, userID)]; accumulator != nil {
			return accumulator
		}
		if deployID != "" {
			if key := runtimeByDeploy[appID+"\x00"+deployID]; key != "" {
				return applicationMap[key]
			}
		}
		if userID != "" {
			if key := runtimeByUser[appID+"\x00"+userID]; key != "" {
				return applicationMap[key]
			}
		}
		keys := runtimeKeysByApp[appID]
		if len(keys) == 1 {
			return applicationMap[keys[0]]
		}
		if len(keys) > 1 {
			return nil
		}
		accumulator, _ := ensureApplication(appID, title, deployID, userID)
		return accumulator
	}
	for _, identity := range containers {
		if identity.AppID == "" {
			continue
		}
		accumulator := resolveApplication(identity.AppID, identity.AppTitle, identity.DeployID, identity.UserID)
		if accumulator == nil {
			continue
		}
		if accumulator.item.InstanceStatus == "" {
			accumulator.item.InstanceStatus = "running"
		}
		if identity.ContainerName != "" {
			accumulator.containers[identity.ContainerName] = struct{}{}
		}
	}
	for _, process := range allProcesses {
		if process.AppID == "" {
			continue
		}
		accumulator := resolveApplication(process.AppID, process.AppTitle, process.DeployID, process.UserID)
		if accumulator == nil {
			continue
		}
		accumulator.item.ReadRate += process.ReadRate
		accumulator.item.WriteRate += process.WriteRate
		accumulator.item.Processes = append(accumulator.item.Processes, process)
		if process.ReadRate > 0 || process.WriteRate > 0 {
			accumulator.item.ActiveProcessCount++
		}
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
		sort.Slice(accumulator.item.Processes, func(i, j int) bool {
			left := accumulator.item.Processes[i].ReadRate + accumulator.item.Processes[i].WriteRate
			right := accumulator.item.Processes[j].ReadRate + accumulator.item.Processes[j].WriteRate
			if left != right {
				return left > right
			}
			return accumulator.item.Processes[i].PID < accumulator.item.Processes[j].PID
		})
		appSources = append(appSources, accumulator.item)
	}
	sort.Slice(appSources, func(i, j int) bool {
		leftRate := appSources[i].ReadRate + appSources[i].WriteRate
		rightRate := appSources[j].ReadRate + appSources[j].WriteRate
		if leftRate != rightRate {
			return leftRate > rightRate
		}
		leftPaused := strings.Contains(strings.ToLower(appSources[i].InstanceStatus), "pause") ||
			strings.Contains(strings.ToLower(appSources[i].InstanceStatus), "stop")
		rightPaused := strings.Contains(strings.ToLower(appSources[j].InstanceStatus), "pause") ||
			strings.Contains(strings.ToLower(appSources[j].InstanceStatus), "stop")
		if leftPaused != rightPaused {
			return !leftPaused
		}
		if appSources[i].AppTitle != appSources[j].AppTitle {
			return appSources[i].AppTitle < appSources[j].AppTitle
		}
		return appSources[i].DeployID < appSources[j].DeployID
	})
	if processPage.Total > 0 {
		page = min(page, (processPage.Total+limit-1)/limit)
	}
	activeProcessTotal := 0
	for _, process := range allProcesses {
		if process.ReadRate > 0 || process.WriteRate > 0 {
			activeProcessTotal++
		}
	}
	offset := (page - 1) * limit
	if offset > len(allProcesses) {
		offset = len(allProcesses)
	}
	end := min(len(allProcesses), offset+limit)
	processes := allProcesses[offset:end]

	writeJSON(w, http.StatusOK, map[string]any{
		"deviceId": deviceID, "collectedAt": processPage.CollectedAt, "processTotal": processPage.Total,
		"activeProcessTotal": activeProcessTotal, "processPage": page, "processPageSize": limit,
		"processes": processes, "applications": appSources, "applicationTotal": len(appSources),
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
