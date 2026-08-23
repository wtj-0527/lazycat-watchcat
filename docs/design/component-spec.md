# Component Specification

## App Shell

### Sidebar

- Desktop width：224 px
- Background：`surface.sidebar`
- Horizontal padding：16 px
- Top padding：24 px
- Active item：`surface.sidebarRaised`，圆角 10 px
- 导航项高度：42 px
- 导航文字与图标必须同时表达当前页面；不得仅依赖颜色

### Brand

- Logo：40 × 40 px，圆角 11 px
- Logo 资源：`assets/cat-eye-logo-64.png`
- Wordmark：`猫眼`
- Subtitle：`Fleet Monitoring`
- 图片使用正方形比例，不拉伸，不重新着色，不叠加 emoji

### Topbar

- Desktop height：72 px
- Background：`surface.card`
- Bottom border：1 px `border.default`
- Title：20 px，Bold
- Subtitle：11 px，Secondary
- Primary action 高 36 px，圆角 8 px

## Layout

- 主内容左边界位于 Sidebar 之后。
- Desktop 内容 padding：40 px 24 px。
- 标准 Grid gap：16 px。
- Stat Grid：6 列；详情和运维场景允许 4 列。
- 主从内容：2:1；详情双栏：1.25:0.75。
- 不在一张折线图中叠加十多台设备；使用 Top N、Small Multiples 或 Matrix。

## Card

- Background：`surface.card`
- Border：1 px `border.default`
- Radius：12 px
- Padding：18 px
- Shadow：轻量 `0 1px 2px rgba(15, 23, 42, 0.03)`
- Card 之间默认间距：16 px

## Stat Tile

- Label：12 px，Secondary
- Value：28 px，Bold
- Hint：11 px
- 状态色只作用于状态或变化，不用于装饰普通指标
- Fleet 首页必须同时展示设备总数、在线、离线、Critical、Warning 和存储／健康风险，不用平均值掩盖异常设备

## Status Pill

- Height 由 5 px vertical padding 和 10 px horizontal padding形成
- Radius：14 px
- Font：11 px，Semibold
- 文案与颜色同时出现
- 语义和颜色见 `status-model.md`

## Table / Matrix

- Header：11 px，Muted
- Cell vertical padding：13 px
- Row separator：1 px `border.subtle`
- Hover 可使用 `surface.canvas`
- 设备名、资源名和状态为主要信息；时间和分组为次要信息
- Unknown、Offline 和 Stale 必须显示明确文字
- 表格较宽时允许水平滚动，不静默截断关键列

## Risk / Alert Row

每条风险至少包含：

- 设备与设备组
- 资源类型和资源身份
- 严重程度
- 首次／最近发生时间或持续时间
- 当前值与单位
- 判断依据
- 推荐操作
- 可访问的原始证据或关联巡检

Critical 使用左侧强调线或图标加文字，不能仅将整行染红。

## Chart

- 不使用双 Y 轴。
- 状态色不作为普通 Series Identity。
- 普通 Series 优先使用 `chart.series` Token。
- Top N 之外折叠为 Other。
- 必须提供 Tooltip、时间范围、Legend；关键图表提供 Table View。
- 空数据和采集中不能绘制误导性的零值折线。

## Typography

Penpot 基线字体为 `Noto Sans SC`。Web fallback：

```css
font-family: "Noto Sans SC", Inter, "PingFang SC", "Microsoft YaHei", system-ui, sans-serif;
```

常用字号为 10、11、12、13、14、15、16、18、20 和 28 px。正文以 12～13 px 为主，避免在高密度表格中低于 11 px。

## Focus and accessibility

- 所有按钮和可点击行必须有 keyboard focus indicator。
- 状态不得只通过颜色表达。
- 图标必须有可访问名称或可见文字。
- 正文和交互文字应满足 WCAG AA 对比度；正式发布前仍需完成 Palette／CVD 自动验证。
