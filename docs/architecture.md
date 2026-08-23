# 生产架构

```text
┌──────────────────────── 猫眼单一 LPK ─────────────────────────┐
│ Web UI ── HTTPS API ── Hub Service                            │
│                         ├─ Device/Pairing/Certificate Service │
│                         ├─ Metrics Ingest & Query              │
│                         ├─ Alert Engine & LazyCat Notify       │
│                         ├─ Inspection Scheduler               │
│                         └─ Retention/Backup/Audit              │
│                                  │                             │
│                         SQLite metadata + time-series store    │
│                                                               │
│ Embedded Collector ── 本机只读指标 ───────────────────────────│
└──────────────────────────────────┬─────────────────────────────┘
                                   │ mTLS / private network
                ┌──────────────────┼──────────────────┐
                ▼                  ▼                  ▼
        远端 Collector      远端 Collector      远端 Collector
        local buffer        local buffer        local buffer
```

默认交付和 nasw 安装只包含一个“猫眼”LPK。本机 Collector 与 Hub 同进程生命周期启动并自动注册；远端 Collector 协议仅作为未来多设备扩展接口，不再作为当前独立 LPK 交付。

## 安全边界

- Collector 主动连接 Hub，Hub 不向设备开放通用远程执行能力。
- 首次配对使用有效期 10 分钟、仅可使用一次的配对码。
- 配对完成后使用独立客户端证书；设备解绑立即吊销证书。
- 所有巡检任务均为版本化白名单任务，不接受脚本或命令字符串。
- 原始证据包含采集时间、Collector 版本、能力和内容摘要。
- Hub API 仅接受 LazyCat 应用入口提供的用户会话。

## 数据策略

- 元数据、配置、告警、巡检和审计使用事务数据库。
- 指标按设备、指标名称和时间组织，批量写入并按时间分区清理。
- 30 天后删除原始指标；每小时生成 min/max/avg/p95 降采样数据。
- Collector 断网时使用有界磁盘队列，超过上限优先保留事件和巡检证据。

## 可用性策略

- Hub 和 Collector 提供 readiness/liveness 接口。
- 数据库迁移必须向前兼容一个版本，并在升级前自动备份。
- Collector 使用指数退避和抖动重连，避免 Hub 恢复时发生惊群。
- 指标携带采集时间；Hub 明确区分正常、离线和数据过期。
- 通知按告警指纹去重，并支持冷却时间和维护窗口。
