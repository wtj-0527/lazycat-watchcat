# Collector P1

## 安全模型

- 首次注册通过 10 分钟一次性配对码完成。
- Hub 为每台设备签发独立 Ed25519 客户端证书，默认有效期一年。
- CA 私钥仅保存在 Hub 数据目录，权限为 `0600`。
- 指标端点仅监听 mTLS 服务，要求 TLS 1.3。
- Hub 同时校验证书签名、证书 CN、设备 ID、证书序列号和吊销状态。
- Collector 仅实现固定的只读采集器，不接受命令、脚本或插件。

## 当前白名单指标

- `system.cpu.cores`
- `system.load.1m`
- `system.memory.usage`
- `system.memory.available`
- `filesystem.root.usage`
- `filesystem.root.available`
- `system.uptime`

## 离线队列

- 指标先写入本地原子 JSON 队列，再尝试发送。
- 默认最多保留 2048 个批次。
- 队列满时丢弃最旧指标，保留最新状态。
- 发送失败时停止本轮刷新并等待下一个采集周期。

## 配置

| 环境变量 | 说明 |
|---|---|
| `MAOYAN_HUB_URL` | Hub 的配对入口 |
| `MAOYAN_COLLECTOR_URL` | Hub 的 mTLS 指标入口 |
| `MAOYAN_PAIRING_CODE` | 首次启动使用的一次性配对码 |
| `MAOYAN_DEVICE_NAME` | 设备显示名称 |
| `MAOYAN_COLLECT_INTERVAL` | 采集周期，最低 10 秒，默认 30 秒 |
| `MAOYAN_COLLECTOR_DATA_DIR` | 凭据和离线队列目录 |

首次配对成功后，证书、私钥和设备令牌会以 `0600` 权限保存。后续启动不再需要配对码。

当前 P1 仍使用环境变量完成首次配置；生产设置页面和自动安装流程将在前端/API 接入阶段完成。
