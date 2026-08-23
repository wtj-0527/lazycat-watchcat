# Design Acceptance Checklist

## Scope and assets

- [x] 运行前端已按设计交接实现；生产数值仍来自真实 API。
- [ ] `assets/cat-eye-logo.png` 是批准的 512 × 512 原始 PNG。
- [x] `assets/cat-eye-logo-64.png` 是 64 × 64 UI PNG。
- [x] Logo 以正方形比例显示，不拉伸、不裁剪主体、不重新着色、不叠加 emoji。
- [x] Penpot 和实现中的品牌文案均为“猫眼 / Fleet Monitoring”。

## App shell

- [x] Desktop Sidebar 宽 224 px，Topbar 高 72 px。
- [ ] Sidebar、Topbar 和内容区的层级与 Board 一致。
- [ ] 当前导航项同时具有背景、图标和文字状态。
- [x] 主内容不产生页面级水平溢出；宽表只在卡片内部滚动。
- [x] Focus indicator 清晰可见，键盘可以访问按钮和可点击设备／报告行。

## Data states

对 Overview、Devices、Applications、Storage、Alerts、Inspections 和 Settings 分别验证：

- [ ] Loading 不显示假 0、假 Healthy 或误导性零值图表。
- [ ] Content 使用真实 API 数据，不硬编码 Penpot 示例。
- [ ] Empty 解释没有数据的含义和可能下一步。
- [ ] Error 显示可理解信息，并允许重试或等待轮询。
- [ ] Stale 保留最近成功快照时显示明确标记和时间。
- [ ] Offline 不继续显示 Healthy。
- [ ] Unknown、Restricted、Unsupported 和 Error 不被合并。

## Fleet overview

- [ ] 同时展示设备总数、在线、离线、Critical、Warning 和健康／存储风险。
- [ ] 不使用 Fleet 平均值掩盖异常设备。
- [ ] 事故态由真实数据驱动，不是单独硬编码页面。
- [ ] 当前风险可追溯到设备、资源、值、单位和时间。
- [ ] `updatedAt` 与设备／指标新鲜度语义一致。

## Devices and applications

- [ ] 设备行可打开详情；返回后恢复设备清单。
- [ ] 设备详情独立处理 loading、error 和 content。
- [ ] 原始指标显示名称、值、单位、标签和采集时间。
- [ ] Applications 的 `stale: true` 显示“最近一次成功快照”。
- [ ] 应用矩阵显示应用身份、设备、运行状态、实例和版本；缺失字段显示 Unknown。

## Storage

- [ ] 容量、温度和 Media Errors 使用不同单位及阈值语义。
- [ ] 温度不可用不显示为 0 ℃。
- [ ] SMART／NVMe 不支持、权限受限和采集失败可以区分。
- [ ] 关键存储风险不只依赖颜色表达。

## Alerts and inspections

- [ ] Firing、Acknowledged、Silenced 和 Resolved 生命周期正确。
- [ ] Acknowledge 与 Silence 失败时不改变本地状态。
- [ ] Silence 明确显示默认 1440 分钟或用户选择的时长。
- [ ] 活动视图默认不展示 Resolved 告警。
- [ ] 手动巡检期间按钮禁用且文案为“巡检中…”。
- [ ] 巡检成功后回读历史；失败不伪造报告。
- [ ] 巡检摘要和 SHA-256 来自真实 API。

## Operations safety

- [ ] 未校验备份的恢复按钮禁用。
- [ ] 恢复前二次确认，并说明应用会重启和短暂断连。
- [ ] 重新开始 7 天观测前二次确认。
- [ ] 未完成 7 天观测时不显示“已通过”。
- [ ] Toast 不是备份、恢复、告警或巡检完成的唯一证据；操作后进行数据回读。

## Charts and accessibility

- [ ] 不使用双 Y 轴。
- [ ] 不把十多台设备叠加到单一折线图。
- [ ] Top N 之外折叠为 Other，或使用 Matrix／Small Multiples。
- [ ] 图表包含 Tooltip、Legend、时间范围；关键图表提供 Table View。
- [ ] 状态用颜色、文字和／或图标共同表达。
- [ ] 正文和交互文字达到 WCAG AA 对比度。
- [ ] 正式发布前完成 Palette／CVD 自动验证；完成前不声称已验证。

## Responsive boundary

- [ ] Desktop Light Theme 与 Penpot Board 完成视觉验收。
- [ ] 现有 `≤1000 px`、`≤720 px` CSS 降级不被描述为正式 Mobile 设计。
- [ ] Dark Mode 和独立 Mobile／Tablet 未完成时，在产品说明中保持明确边界。

## Repository verification

```bash
python3 -m json.tool docs/design/tokens.json >/dev/null
file docs/design/assets/cat-eye-logo.png docs/design/assets/cat-eye-logo-64.png
git diff --check
git status --short
```

实现交付会同时修改 `frontend/`、构建后的 `web/`、版本文件和本目录文档。
