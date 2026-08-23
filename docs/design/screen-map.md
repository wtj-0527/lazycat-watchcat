# Screen Map

## Penpot Board 与现有页面映射

| Penpot Board | 场景 | 当前入口 | 实现说明 |
|---|---|---|---|
| `00 · Design System` | Token、状态色和基础组件 | 无路由 | 供开发与验收引用 |
| `01 · Fleet Overview / Normal` | Fleet 正常态 | `#overview` | 统计卡、设备健康矩阵、当前风险和数据新鲜度 |
| `02 · Fleet Overview / Incident` | Fleet 事故态 | `#overview` | 与正常态共用页面，由实时数据驱动状态变化 |
| `03 · Devices / Fleet Inventory` | 设备清单 | `#devices` | 设备列表与接入数量 |
| `04 · Device Detail / Overview` | 设备详情概览 | `#devices` 后选择设备 | 当前实现通过页内状态切换，不使用独立 URL |
| `05 · Device Detail / Storage & SMART` | 单设备存储详情 | 设备详情／`#storage` | 原型表达目标信息层级；当前实现把 Fleet 存储集中在 `#storage` |
| `06 · Applications / Fleet Matrix` | LPK 应用矩阵 | `#apps` | 展示状态、实例和版本分布 |
| `07 · Storage Health / Fleet` | Fleet 存储健康 | `#storage` | 文件系统、温度和 NVMe Media Errors |
| `08 · Alerts / Triage` | 告警处置 | `#alerts` | 活动告警、确认和静默 |
| `09 · Inspections / Report` | 巡检与证据 | `#inspections` | 手动巡检、历史和 SHA-256 摘要 |
| `10 · Settings / Device Onboarding` | 设备接入与生产运维 | `#settings` | 设备接入、能力、通知、维护、保留和审计使用 Tab；一次性配对码调用真实 API |

## 全局框架

- Sidebar：左侧固定，Desktop 宽 224 px。
- Topbar：主内容顶部粘性，Desktop 高 72 px。
- Content：位于 Topbar 下方；以 1440 px 画布为视觉基线。
- 全局导航：总览、设备、应用、存储健康、告警、巡检、设置。
- 品牌区：40 × 40 px 猫头鹰 Logo、`猫眼` Wordmark、`Fleet Monitoring` 副标题。
- Hub 状态：Sidebar 底部展示 Monitor Hub 运行状态。

## 页面状态

每个数据页面都必须有以下独立状态：

1. Loading：数据未返回，不提前展示 0 或健康。
2. Content：真实数据可用。
3. Empty：请求成功但结果为空，说明为空原因和下一步。
4. Error：请求失败，显示可理解的错误信息。
5. Stale：存在最近成功快照但当前采集失败，明确标记数据已过期。
6. Offline／Unknown：按状态模型展示，不能回退为 Healthy。

## 响应式边界

当前 Penpot 仅完成 Desktop Light Theme。现有 CSS 中的 `1000 px` 和 `720 px` media query 是工程降级规则，不是已签字的 Mobile 设计：

- `≤1000 px`：Stat Grid 两列，双栏内容改为单栏，Sidebar 缩至 190 px。
- `≤720 px`：Sidebar 缩至 68 px，隐藏部分文字与 Hub 卡片，Stat Grid 两列。

如需正式移动端交付，应新增独立 Board 和验收基线。
