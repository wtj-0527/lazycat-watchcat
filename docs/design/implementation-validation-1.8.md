# 猫眼 1.8.0 Penpot 实现验收

日期：2026-08-23  
视觉基线：Penpot `猫眼 · Fleet Monitoring · V1`，Desktop Light 1440 × 1024  
数据基线：生产 API 与实时 Collector 状态

## 本轮修正

- 使用批准的蓝金色猫头鹰 Logo，移除 emoji 品牌图标。
- 按 224 px Sidebar、72 px Topbar、设计 Token 和 Fleet-first 信息层级重构 App Shell。
- Overview 同时呈现设备、在线、离线、Critical、Warning、存储风险和八列设备健康矩阵。
- 增加真实数据驱动的 Incident banner；不硬编码 Penpot 事故示例。
- Devices 增加搜索、状态筛选、完整清单列和设备详情 Tab。
- Applications 改为跨设备应用矩阵，并使用真实 Package Manager 实例及容器资源。
- Storage 增加 Fleet 摘要、风险优先级、设备卡和采集能力语义。
- Alerts 增加总数、严重度、生命周期筛选和 Triage 层级。
- Inspections 增加正式报告头、分类结果、证据摘要和历史选择。
- Settings 改为 Device Onboarding/能力/通知/维护/保留/审计 Tab，并接入真实一次性配对码 API。
- 保留备份恢复、7 天稳定性观测、LazyCat 系统通知和单一 LPK 部署。

## 数据诚实性

- 不使用 Penpot 示例设备、数值、告警或报告数据。
- 当前 API 缺少设备组、标签、位置、历史增长率、可配置阈值、维护窗口和独立事件流时，明确显示 `Unknown` 或 `Contract gap`。
- Restricted、Unsupported、Error、Stale、Offline 与 Healthy 不合并。
- 全局“最近 24 小时”在 API 尚未统一支持时保持禁用，不伪造筛选生效。
- 导出 JSON/PDF 在没有正式导出接口时保持禁用。

## 自动验证

```bash
go test ./... -count=1
go vet ./...
npm --prefix frontend test -- --run
npm --prefix frontend run build
lzc-cli project lint .
lzc-cli lpk lint lazycat-maoyan-1.8.0.lpk
```

## nasw 实机确认

- 已安装 `lazycat-maoyan-1.8.0.lpk`，Package Manager 显示猫眼 `running`、版本 `1.8.0`。
- `/api/v1/health`、`version`、`settings`、`overview`、`applications`、
  `operations`、`database/status` 和 `stability` 均返回 HTTP 200。
- Package Manager 实时返回 45 个应用实例；猫眼自身只有一个 running 实例。
- 应用列表中不存在独立 Collector；nasw 只有一个猫眼业务进程，
  本机 Collector 由该进程内置运行。
- 主机/容器指标、能力状态、告警、巡检、备份和稳定性数据均正常回读。
- Sidebar 实测 224 px、Topbar 实测 72 px、Logo 实测 40 × 40 px，
  页面 body 无水平溢出；宽表在卡片内部滚动。
- 1.7.0 → 1.8.0 升级前备份已自动创建并通过 SHA-256 校验。
- Palette/CVD 自动化尚未完成，因此不声称已完成正式色觉无障碍认证。
