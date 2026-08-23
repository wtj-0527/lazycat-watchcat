# lazycat-maoyan

猫眼是单 LPK 交付的 LazyCat 设备健康监控应用。

- Hub、Web UI、SQLite、告警、巡检、通知和本机 Collector 全部包含在 `lazycat-maoyan-<version>.lpk`
- 安装后自动注册当前 LazyCat 设备并开始采集，无需再安装第二个 Collector 应用
- 保留只读远端 Collector 协议代码，为后续跨设备接入保留兼容能力，但不再单独交付第二个 LPK
