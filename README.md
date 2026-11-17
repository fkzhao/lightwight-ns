# lightwight-ns

Go 服务注册中心与管理后台的第一版。管理员通过 Web 手动登记微服务、机器 IP 和实例端口；本机 agent 可通过只读 RPC 获取带版本号的服务清单。agent 本身尚未实现。

## 当前模型

- **服务**：唯一的服务名称和说明。
- **机器**：唯一的机器名称、IP 地址和说明。
- **实例**：一个服务部署在一台机器的一个端口上。相同机器的端口不能重复分配，停用的实例不会出现在 RPC 发现结果里。
- **清单**：每次成功修改都会递增版本号，并持久化到 JSON 文件。

## 本地运行

需要 Go 1.22+ 和 Node.js 22.13+。

```bash
go run ./cmd/center
```

另开终端运行前端开发服务器：

```bash
cd web
npm ci
npm run dev
```

开发时访问 Vite 输出的地址，`/api` 会代理到 Go 服务。也可以先运行 `npm run build`，再从项目根目录启动 Go 服务，直接访问 `http://127.0.0.1:8080`。此时 Go 会提供 `web/dist` 的静态页面。

## 接口

管理接口默认监听 `127.0.0.1:8080`：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/snapshot` | 获取完整服务清单 |
| POST | `/api/services`、`/api/machines`、`/api/instances` | 创建记录 |
| PUT | `/api/{资源}/{id}` | 修改记录 |
| DELETE | `/api/{资源}/{id}` | 删除记录 |

agent 使用的 `net/rpc` 默认监听 `127.0.0.1:9090`，服务名为 `Registry`：

- `Registry.GetSnapshot`：传入 `registry.SnapshotRequest{KnownVersion}`；版本未变化时 `Changed=false`。每次调用使用新的响应对象。
- `Registry.Discover`：传入 `registry.DiscoverRequest{ServiceName}`，返回已启用实例的地址和端口。

RPC 请求和响应类型位于 `internal/registry/rpc.go`，同一 Go module 内的 agent 可以直接复用。

## 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `NS_DATA_FILE` | `data/registry.json` | 清单文件 |
| `NS_HTTP_ADDR` | `127.0.0.1:8080` | Web 与管理 API |
| `NS_RPC_ADDR` | `127.0.0.1:9090` | agent 读取 RPC |
| `NS_WEB_DIST` | `web/dist` | 已构建的前端目录 |

当前是单中心、单文件存储的 MVP。管理接口和 RPC 尚未加入认证；部署到多机环境时，应先通过受控网络或网关保护接口，再实现中心高可用和 agent 缓存。
