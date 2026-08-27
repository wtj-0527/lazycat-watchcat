# Logo Assets

## Files

| File | Size | Purpose |
|---|---:|---|
| `watchcat-logo.png` | 512 × 512 | 用户批准的橙色猫与 NAS 监控图像 |
| `watchcat-logo-64.png` | 64 × 64 | Sidebar、导航和紧凑 UI 使用的 PNG |

## Usage

- 默认显示尺寸：40 × 40 px。
- 默认圆角：11 px。
- 保持 1:1 比例；禁止非等比拉伸。
- 保留原图浅紫背景、蓝金配色和猫头鹰主体。
- 不抠图、不重绘、不重新着色、不叠加 emoji 或额外图标。
- 在高分辨率或需要更大尺寸时使用原始 512 px 文件。
- 在常规 App Shell 中优先使用 64 px 文件，避免加载不必要的大图。

## HTML example

```html
<img
  src="/path/to/watchcat-logo-64.png"
  width="40"
  height="40"
  alt="WatchCat"
/>
```

本目录资产用于设计交付。根目录 `icon.png` 和 LPK manifest 图标不在本次变更范围内。
