# lazycat-watchcat

WatchCat 是单 LPK 安装、单镜像 Service 运行的 LazyCat 设备健康监控应用。

- Hub、Web UI、SQLite、告警、巡检、通知和本机 Collector 全部运行在同一个 `watchcat` Service
- LPK 只包含应用清单和图标，运行镜像发布到阿里云 ACR：`registry.cn-shanghai.aliyuncs.com/wtjking/lazycat-watchcat`
- Web UI 使用 Vue 3、Vite 和 TypeScript 组件化实现，生产包只包含编译后的静态资源
- 应用状态通过官方 LzcSDK Package Manager API 获取，按当前 LazyCat 用户身份查询并保存最近一次成功快照
- 主机扩展指标使用 gopsutil 读取 CPU、负载、Swap、块设备 IO、逐接口网络 IO 和可见温度传感器
- 风扇转速仅调用 LazyCat HAL `GetFanRpm` 只读接口，不提供风扇控制
- 容器指标只通过 LazyCat Docker socket 调用容器列表和 Stats，并按 `lzcapp.app-id` 聚合到应用
- 容器运行状态每 30 秒更新；资源 Stats 采用 8 容器轮转批次，完整 Fleet 在约 5 分钟内刷新，避免大规模实例同时采样拖高系统负载
- 安装后自动注册当前 LazyCat 设备并开始采集，无需再安装第二个 Collector 应用
- 每个 LPK 均可生成一次性设备邀请，或在“接入”页面粘贴邀请加入现有 WatchCat
- 加入后继续保留本机监控，同时使用持久凭据和离线队列向主 WatchCat 上报同一批真实指标
- 跨设备配对与指标上报共用同一个可达的 WatchCat HTTP 地址，不再要求单独暴露 mTLS 指标端口

## 镜像与 LPK 发布

镜像使用不可变版本标签，LPK 清单固定引用该标签，避免 `latest` 导致不可审计升级：

```bash
docker build \
  --label org.opencontainers.image.version=1.0.4 \
  -t registry.cn-shanghai.aliyuncs.com/wtjking/lazycat-watchcat:1.0.4 .
docker push registry.cn-shanghai.aliyuncs.com/wtjking/lazycat-watchcat:1.0.4
# 将 lzc-manifest.yml 的 image 固定为 push 返回的 sha256 digest
lzc-cli project lint .
lzc-cli project deploy --release
```

## 生产运维能力

- SQLite 在线一致性备份、SHA-256 校验和 `quick_check` 完整性验证
- 版本切换前自动备份，恢复前自动创建安全备份，恢复通过重启阶段原子应用
- 应用内提供备份列表、手动备份、恢复入口和数据库状态
- 数据库备份保留份数可配置，支持逐份校验后恢复或手动删除
- 镜像管理区区分可批量清理的悬空旧镜像与可能供未启动 LPK 使用的缓存镜像；缓存镜像仅允许分页查看后逐个确认删除
- 持久化 7 天稳定性观测：每分钟记录数据库完整性、延迟、指标新鲜度、通知积压和保留任务状态
- 全局侧栏和设置页显示统一构建版本

备份位于应用数据目录的 `backups/`，默认最多保留最近 20 份。恢复旧版本数据库后，
当前版本会重新执行幂等迁移。真实 7 天稳定性资格只有在完整观测周期结束且期间没有采样失败后才会标记为通过。

## 权限边界

- 单一 LPK、单一 Service 进程，不使用 `privileged`、host PID、host network 或宿主机目录写挂载
- `PERM_LZC_DOCKER_ADMIN` 仅用于让 LazyCat Runtime 映射 Docker socket；WatchCat 客户端只允许固定 GET 路径
- 不申请 `PERM_OTHER_APP_DATA_ADMIN`，不读取其他应用的数据目录
- SMART、NVMe 和 Btrfs 未获得所需只读设备／挂载映射时报告为 `restricted`；已配置但采集或解析失败时报告为 `error`
