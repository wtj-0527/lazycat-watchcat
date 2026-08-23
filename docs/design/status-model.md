# Status Model

状态模型用于统一 Penpot、前端、API 和验收语义。颜色只是表达手段，状态文字和上下文原因不可省略。

## 1. Health

| 状态 | 含义 | UI 文案 | 颜色 Token | 可聚合为健康 |
|---|---|---|---|---|
| `Healthy` | 已获得足够且新鲜的证据，指标在规则范围内 | 健康 | `status.healthy` | 是 |
| `Warning` | 存在退化、接近阈值或非紧急异常 | 警告 | `status.warning` | 否 |
| `Critical` | 已超过严重阈值，或存在明确数据／服务风险 | 严重 | `status.critical` | 否 |
| `Unknown` | 证据不足，不能判断健康状态 | 未知 | `status.unknown` | 否 |

`Unknown` 绝不等于 `Healthy`。没有指标、字段缺失、采集尚未开始或能力状态不明时均应使用 Unknown 并说明原因。

## 2. Connectivity

| 状态 | 含义 | UI 文案 |
|---|---|---|
| `Online` | 最近心跳／采集时间在新鲜度阈值内 | 在线 |
| `Stale` | 有最近成功快照，但当前数据已超过新鲜度阈值 | 数据已过期 |
| `Offline` | 设备在离线阈值内没有心跳，或连接明确失败 | 离线 |

连接状态与健康状态是两个维度。离线设备不能继续展示上次的 Healthy；如展示历史健康，必须同时标记 Stale／Offline 和快照时间。

## 3. Capability

| 状态 | 含义 | UI 文案 |
|---|---|---|
| `Available` | 所需接口可调用且返回有效数据 | 可用 |
| `Restricted` | 接口存在，但当前身份／LPK 无权限 | 权限受限 |
| `Unsupported` | 当前系统、硬件或版本不支持该能力 | 不支持 |
| `Error` | 能力理论上可用，但调用或解析失败 | 采集错误 |

不得把 `Restricted`、`Unsupported`、`Error` 合并为同一个“不可用”。这些状态分别对应授权、兼容性和故障处置。

## 4. Inventory

| 状态 | 含义 | UI 文案 |
|---|---|---|
| `Present` | 已在最新有效清单中发现资源 | 已发现 |
| `Missing` | 曾存在的资源在最新清单中缺失 | 缺失 |

新设备从未返回某类资源时优先使用 Unknown／Unsupported，不直接判定 Missing。

## 5. Fleet overall priority

当页面必须将多维信息压缩成一个总体状态时，使用以下优先级：

```text
Critical > Offline > Warning > Unknown > Healthy
```

同时保留原始维度。例如总体显示 Offline 时，详情仍应提供最后一次健康判断和最后在线时间。

## 6. Alert lifecycle

```text
Firing → Acknowledged → Resolved
              ↘ Silenced
```

- `Firing`：规则当前成立，需处理。
- `Acknowledged`：用户已确认，但问题未必恢复。
- `Silenced`：在明确期限内抑制通知，不等于恢复。
- `Resolved`：规则已不再成立；活动列表默认不展示。

严重程度与生命周期相互独立。例如 Critical 告警可以处于 Acknowledged，但仍然是 Critical。

## 7. Current implementation compatibility

当前 `frontend/src/types.ts` 已将状态维度拆分为：

```text
Health: healthy | warning | critical | unknown
Connectivity: online | stale | offline
Capability: available | restricted | unsupported | error | unknown
```

当前实现处理：

| 输入／场景 | 当前模型处理 |
|---|---|
| 在线且证据新鲜、无规则命中 | Health = Healthy；Connectivity = Online |
| 在线且命中告警规则 | Health = Warning／Critical；Connectivity = Online |
| 心跳过期或设备撤销 | Health = Unknown；Connectivity = Stale／Offline；保留离线告警证据 |
| 能力接口成功并返回有效数据 | Capability = Available |
| 所需只读设备、挂载或权限未映射 | Capability = Restricted |
| 当前硬件／系统没有该能力 | Capability = Unsupported |
| 已配置能力但调用或解析失败 | Capability = Error |
| 尚无足够证据分类 | Capability = Unknown |

`StatusPill` 暂时保留 `degraded`、`unavailable` 的旧值文案与样式，仅用于兼容历史数据；新 Capability producer 不再产生这两个值。API 消费方不得根据旧颜色推断目标五态。
