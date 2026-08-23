# Interaction Specification

## Navigation

当前实现使用 hash navigation：

```text
#overview
#devices
#apps
#storage
#alerts
#inspections
#settings
```

- 未识别的 hash 回退到 `#overview`。
- 当前导航项必须同时具有 active background 和可见文字状态。
- 设备详情目前是 `#devices` 内的页内状态；返回后回到设备清单。

## Refresh and freshness

- 数据页默认每 30 秒刷新一次。
- 刷新不应清空当前可用内容并造成整页闪烁。
- `lastSeenAt`、`collectedAt` 和页面 `updatedAt` 必须使用同一时间语义。
- 超过连接新鲜度阈值时显示 Stale／Offline，不继续显示 Healthy。
- Applications API 返回 `stale: true` 时，必须标记“最近一次成功快照”。

## Global actions

### 时间范围

Topbar 的“最近 24 小时”是时间范围入口。当前 API 尚未为全部页面实现时间范围查询时，不得伪造筛选生效；应隐藏、禁用或明确标记未实现。

### 开始巡检

- Topbar 主按钮导航到巡检页。
- “立即巡检”调用 `POST /api/v1/inspections`。
- 请求期间按钮禁用并显示“巡检中…”。
- 成功后刷新巡检历史并显示 Toast。
- 失败显示后端错误，不伪造成功状态。

## Device interactions

- 点击设备行加载 `GET /api/v1/devices/{id}`。
- 加载中、错误和详情内容相互独立。
- 返回设备清单不应触发破坏性操作。
- 详情中的原始指标保留指标名、值、单位、标签和采集时间。

## Alert interactions

告警生命周期：

```text
Firing → Acknowledged → Resolved
              ↘ Silenced
```

- Acknowledge 和 Silence 使用明确动作文案。
- Silence 当前默认 1440 分钟；UI 必须让用户知道静默时长。
- 操作成功后刷新列表并显示 Toast。
- 操作失败保持原状态并显示错误。
- Resolved 不应继续作为活动告警展示，除非页面明确处于历史视图。

## Backup and restore

- “立即备份”调用 `POST /api/v1/backups`。
- 恢复前必须二次确认。
- 未通过校验的备份不得启用恢复操作。
- 恢复请求成功后应用会重启；UI 需说明连接可能暂时中断。
- 本设计交付不改变这些运维安全约束。

## Stability reset

- 重新开始 7 天稳定性观测是有副作用操作，必须确认。
- 成功后重新读取状态。
- 不得把尚未完成 7 天观测显示为“已通过”。

## Loading, Empty, Error

### Loading

- 使用稳定的 Page State，避免布局跳动。
- Loading 期间不要显示假 0。

### Empty

- 说明“没有数据”而不是“系统正常”。
- 提供可能原因，如未接入设备、Package Manager 未返回数据或尚无巡检记录。

### Error

- 显示可理解的错误文案。
- 保留最近成功快照时同时展示 Stale 标记。
- 不把权限错误、不支持和连接失败合并成一个灰色状态。

## Toast

- 用于短暂反馈非阻断操作结果。
- 不作为备份恢复、巡检证据或告警状态的唯一确认；页面数据回读才是最终结果。
