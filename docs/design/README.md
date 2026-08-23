# 猫眼 Design Handoff

本目录是“猫眼 Fleet Monitoring”V1 的设计交付包，供前端、后端和验收人员把 Penpot 原型转换为可验证实现。

## 基线

- 产品：猫眼 Fleet Monitoring
- 设计文件：Penpot 页面 `猫眼 · Fleet Monitoring · V1`
- 设计范围：Desktop Light Theme
- 参考画布：1440 × 1024 px
- 设备规模：10～50 台 LazyCat 设备；实现允许扩展至 100 台
- 品牌标志：用户确认的蓝金色卡通猫头鹰
- 页面结构：固定 Sidebar、粘性 Topbar、主内容区

## Source of truth

发生冲突时按以下顺序处理：

1. 本目录的状态语义与交互约束
2. Penpot 中对应 Board 的视觉布局
3. `tokens.json` 中的设计 Token
4. 当前实现

状态语义优先于颜色和截图。`Unknown`、无权限、不支持与离线不得合并为“健康”。

## 文件

- [screen-map.md](screen-map.md)：Penpot Board、当前 hash route 与页面状态映射
- [component-spec.md](component-spec.md)：布局、组件和视觉规范
- [interaction-spec.md](interaction-spec.md)：导航、刷新、操作和异常状态
- [status-model.md](status-model.md)：健康、连接、能力、库存和告警语义
- [api-screen-map.md](api-screen-map.md)：当前 API 与页面消费关系
- [acceptance-checklist.md](acceptance-checklist.md)：设计实现验收清单
- [tokens.json](tokens.json)：机器可读的颜色、字体、间距、圆角和布局 Token
- [assets/](assets/)：批准使用的 Logo 资源

## 实现边界

本次交付只新增文档和 Logo 资产，不修改前端、后端、构建、LPK 配置或根目录 `icon.png`。

当前不作为 V1 设计完成项：

- Dark Mode
- 独立 Mobile／Tablet 版式
- 正式 Penpot Components／Variables 发布
- Palette accessibility／CVD 自动化验证
- 新增或修改后端 API

现有实现已包含窄屏 CSS 降级规则，但它不等同于经过设计验收的移动端方案。

## 开发使用方法

1. 先阅读 `status-model.md`，固定业务语义。
2. 根据 `screen-map.md` 找到页面和状态。
3. 使用 `tokens.json` 与 `component-spec.md` 实现视觉层。
4. 根据 `api-screen-map.md` 复用当前 API，不从截图猜字段。
5. 按 `acceptance-checklist.md` 在真实应用中验收。

不要把 Penpot 中的示例数值硬编码到生产 UI。示例设备名、告警和图表数据仅用于表达布局及状态。
