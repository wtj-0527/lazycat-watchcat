# 前端架构

WatchCat 1.5.0 起使用 Vue 3、Vite 和 TypeScript。

## 目录

- `frontend/src/App.vue`：全局布局、导航、版本和 Toast
- `frontend/src/pages/`：总览、设备、应用、存储、告警、巡检和设置页面
- `frontend/src/components/`：状态、统计卡片、设备表格和通用页面状态
- `frontend/src/api.ts`：统一 API 客户端与错误处理
- `frontend/src/composables.ts`：页面生命周期和定时刷新
- `web/`：Vite 生成的生产静态资源

## 构建验证

```bash
npm --prefix frontend ci --include=dev
npm --prefix frontend run check
npm --prefix frontend test
npm --prefix frontend run build
```

LPK 构建脚本会先执行确定性前端安装与生产构建，再编译 Go Hub。SPA 入口不缓存，
内容哈希资源使用长期不可变缓存，避免 LPK 升级后浏览器继续运行旧版前端。

“应用”页面由后端使用官方 LzcSDK `PackageManager.QueryApplication` 查询。后端只转发
LazyCat 网关提供的当前用户身份，不接受前端传入任意用户 ID；成功结果写入
`application_runtime_state`，Package Manager 短暂不可用时显示最近一次成功快照。
