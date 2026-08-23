# API to Screen Map

本文件记录当前仓库已经提供并由 Vue 页面消费的接口。设计实现优先复用这些接口；缺少字段时先明确差距，不从 Penpot 示例数据推导后端契约。

## Common behavior

- 所有路径均为同源相对路径。
- JSON 请求使用 `Content-Type: application/json`。
- 非 2xx 响应优先显示 `error.message`，否则回退为 `HTTP {status}`。
- 时间字段按 ISO timestamp 处理，并在 UI 统一格式化。
- 轮询页面默认约 30 秒刷新；用户操作成功后立即回读。

## Page mapping

| 页面 | Method | Path | 主要数据／用途 |
|---|---|---|---|
| Overview | `GET` | `/api/v1/overview` | `stats`、`devices`、`alerts`、`updatedAt` |
| Devices list | `GET` | `/api/v1/overview` | 复用 `devices` 清单 |
| Device detail | `GET` | `/api/v1/devices/{id}` | 设备元数据、连接／健康状态、`latest` 指标 |
| Applications | `GET` | `/api/v1/applications` | `items`、`source`、`stale` |
| Storage | `GET` | `/api/v1/storage` | `items` 指标、`updatedAt` |
| Alerts | `GET` | `/api/v1/alerts` | 当前告警清单 |
| Alert acknowledge | `POST` | `/api/v1/alerts/{fingerprint}/acknowledge` | 确认告警 |
| Alert silence | `POST` | `/api/v1/alerts/{fingerprint}/silence` | Body：`{"durationMinutes": 1440}` |
| Inspections | `GET` | `/api/v1/inspections` | `items` 巡检历史 |
| Run inspection | `POST` | `/api/v1/inspections` | 创建一次巡检 |
| Settings | `GET` | `/api/v1/settings` | 版本、部署模式、采集周期、保留策略、通知和存储统计 |
| Operations | `GET` | `/api/v1/operations` | 能力与巡检计划 |
| Database status | `GET` | `/api/v1/database/status` | 大小、完整性、备份数量和最近备份 |
| Backups | `GET` | `/api/v1/backups` | `items` 备份清单 |
| Create backup | `POST` | `/api/v1/backups` | 创建在线一致性备份 |
| Restore backup | `POST` | `/api/v1/backups/{name}/restore` | 恢复已校验备份；应用随后重启 |
| Stability | `GET` | `/api/v1/stability` | 7 天稳定性观测状态 |
| Reset stability | `POST` | `/api/v1/stability/reset` | 清零并重新开始观测 |

路径参数 `id`、`fingerprint` 和 `name` 必须使用 URL encoding。

## Data contracts used by design

### Metric

```ts
interface Metric {
  deviceId?: string
  deviceName?: string
  name: string
  value: number
  unit: string
  labels: Record<string, string> | null
  collectedAt: string
}
```

指标展示必须保留 `name`、`value`、`unit`、`labels` 和 `collectedAt`。没有单位时显示明确占位，不自行猜单位。

### Device

```ts
interface Device {
  id: string
  name: string
  hostname: string
  osVersion: string
  collectorVersion: string
  status: string
  lastSeenAt: string
  online: boolean
  stale: boolean
  health: Health
  latest: Record<string, Metric[]>
}
```

连接状态应优先由后端状态、`online`、`stale` 和 `lastSeenAt` 共同表达，不只依赖 `health` 字符串。

### Alert

```ts
interface Alert {
  fingerprint: string
  deviceName: string
  severity: Health
  resource: string
  message: string
  value: number
  unit: string
  status: string
  lastSeenAt?: string
  observedAt?: string
  collectedAt?: string
}
```

当前接口可支持基础告警列表。Penpot 中的设备组、首次发生时间、判断依据、推荐操作和巡检证据若后端尚未提供，应标记为 contract gap，不得用示例文案替代真实数据。

### Overview

```ts
interface Overview {
  stats: Record<string, number>
  devices: Device[]
  alerts: Alert[]
  updatedAt: string
}
```

Stat Tile 应从 `stats` 的真实聚合值读取。字段不存在时显示 Unknown／不可用，不默认为 0。

## Known contract gaps

以下内容出现在 Fleet-first 目标设计中，但当前 API／页面未完整实现：

- 设备分组、标签、位置和版本分布筛选
- 全页面统一的时间范围查询
- 多设备历史趋势和 Small Multiples 数据
- 告警首次发生、持续时间、判断依据、推荐操作和原始证据链接
- 独立的设备接入／配对流程
- 健康、连接、能力和库存四维状态的完整枚举
- Fleet 级远端 Collector 管理

差距不属于本次文档提交的代码实现范围。开发时应先定义 API Schema、错误语义和兼容策略，再接入视觉组件。

## Current deployment boundary

当前仓库交付为单 LPK：Hub、Web UI、SQLite、告警、巡检、通知和本机 Collector 同包运行，并自动注册本机设备。代码保留远端 Collector 协议兼容能力，但 Fleet 远端接入不是本次设计文档可以宣称已经完成的功能。
