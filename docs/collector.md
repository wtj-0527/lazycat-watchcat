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
- `system.cpu.usage`
- `system.load.1m`
- `system.load.5m`
- `system.load.15m`
- `system.memory.usage`
- `system.memory.available`
- `system.swap.usage`
- `system.swap.used`
- `system.swap.total`
- `filesystem.root.usage`
- `filesystem.root.available`
- `system.uptime`
- `network.receive.bytes_total`
- `network.transmit.bytes_total`
- 块设备累计读写字节与操作次数
- 逐接口网络流量、错误和丢包
- 可见硬件温度传感器
- LazyCat HAL 风扇转速
- LazyCat 容器 CPU、内存、网络和块 IO，并按应用 ID 聚合
- NVMe 温度、寿命、备用空间、Media Errors 和 Critical Warning
- ATA Reallocated Sector Count
- Btrfs 容量和使用率
- LPK 应用健康状态及重启次数

SMART 调用仅使用固定的 `smartctl -j -a` 参数，并在明确识别到 USB-SATA 桥接错误时
回退 `-d sat`。设备路径仅接受 `/dev/sdX` 和 `/dev/nvmeXnY` 格式。Btrfs 挂载路径
必须是绝对路径。缺少工具或权限时，Collector 会报告能力降级，但不会影响基础指标上报。

内置 Collector 连接 `/lzcapp/run/lzc-docker/docker.sock`：

- `GET /containers/json`
- `GET /containers/{id}/stats?stream=false`
- 每 5 分钟创建短生命周期 SMART helper，完成后立即删除

SMART helper 使用当前猫眼镜像，不增加常驻 Service，并强制：

- `network_mode=none`
- 只读根文件系统
- `no-new-privileges`
- 默认丢弃全部 capability
- SATA/USB-SATA 仅附加 `SYS_RAWIO`
- NVMe 仅附加 `SYS_RAWIO` 与 `SYS_ADMIN`
- 每次仅映射白名单匹配的单个块设备；NVMe 同时映射对应控制器
- 固定 64 MiB 内存和 32 PID 上限
- 不接受来自用户、API 或数据库的命令、镜像和设备路径

设备发现 helper 仅只读挂载 `/sys`，执行内置固定脚本列举顶层 `sdX` 与 `nvmeXnY`。
SMART helper 不使用 `privileged`，也不挂载完整 `/dev`。

猫眼不开放任意 Docker API 代理。除上述受控 helper 生命周期外，不执行 Docker 写操作。

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
| `MAOYAN_SMART_DEVICES` | SMART 白名单设备，逗号分隔 |
| `MAOYAN_BTRFS_MOUNTS` | Btrfs 白名单挂载点，逗号分隔 |
| `MAOYAN_LPK_STATUS_FILE` | LazyCat Runtime 提供的只读应用状态 JSON 文件 |
| `MAOYAN_DOCKER_SOCKET` | LazyCat Docker socket，默认 `/lzcapp/run/lzc-docker/docker.sock` |

首次配对成功后，证书、私钥和设备令牌会以 `0600` 权限保存。后续启动不再需要配对码。

当前 P1 仍使用环境变量完成首次配置；生产设置页面和自动安装流程将在前端/API 接入阶段完成。

## 证书生命周期

- Collector 证书默认有效期一年。
- 距离到期不足 30 天时自动申请轮换。
- 新证书签发后，旧证书保留 24 小时宽限期，避免响应丢失导致设备锁死。
- Hub 可吊销设备；吊销操作会同时禁用该设备全部证书。
