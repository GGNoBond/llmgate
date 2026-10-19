# LLMGate

> 一个 OpenAI 协议兼容的轻量级 AI 网关：统一接入多家大模型，提供路由、负载均衡、故障转移、熔断、多租户鉴权、Token 限流计费、SSE 流式转发、缓存与可观测性。

## ✨ 特性

- 🔌 **OpenAI 协议兼容**（`/v1/chat/completions`），客户端一行改 `base_url` 即可接入
- 🔀 **多上游路由**：一个模型可挂多个上游实例，支持加权轮询 / 最少连接负载均衡
- 🛡️ **高可用**：定时健康检查 + 请求级故障转移 + 三态熔断（closed/open/half-open）
- 🔑 **多租户鉴权** + 上游 Key 池（Key 遇 429 自动冷却轮换）
- 💰 **Token 计量计费**：Redis + Lua 令牌桶限流（RPM/TPM 双维度、按租户隔离）+ 余额扣减 + 用量落库（可轮转、可关闭）
- 🌊 **SSE 流式转发**：OpenAI SSE ⇄ Ollama NDJSON 双向协议转换，流式场景同样计量 Token
- ⚡ **精确响应缓存**：跨租户共享、TTL 抖动防雪崩（语义缓存见 [Roadmap](#-roadmap)）
- 📊 **可观测性**：Prometheus 指标 + Grafana 预置看板 + traceID 全链路贯穿
- 🐳 **docker-compose 一键起**：gateway + redis + ollama + prometheus + grafana 全栈

## 🏗️ 架构

### 一次请求穿过网关

```mermaid
flowchart LR
    C[客户端] -->|OpenAI 协议| MW[中间件链]
    MW --> MW1[Recovery]
    MW1 --> MW2[TraceID]
    MW2 --> MW3[Auth 多租户鉴权]
    MW3 --> H[Handler]
    H --> B{余额/RPM/TPM 限流}
    B -->|拒绝| E[402/429]
    B -->|放行| CA{缓存命中?}
    CA -->|hit| R[直接返回]
    CA -->|miss| RT[Router 按模型路由]
    RT --> LB[负载均衡 + 熔断/健康过滤]
    LB --> P[Provider 协议转换]
    P -->|OpenAI| U1[上游A]
    P -->|Ollama NDJSON| U2[上游B]
    U1 --> M[Token计量 + 用量落库 + Prometheus]
    U2 --> M
    M --> R
```

### 部署拓扑（docker-compose）

```mermaid
flowchart TB
    Client[客户端 / OpenAI SDK] -->|:8080| GW[llmgate 网关]
    GW -->|限流/余额/缓存| Redis[(redis :6379)]
    GW -->|OpenAI & Ollama 协议| Ollama[ollama :11434<br/>qwen2.5:1.5b]
    Prom[prometheus :9090] -->|抓取 /metrics| GW
    Grafana[grafana :3000<br/>预置看板] --> Prom
```

## 🚀 快速开始

```bash
# 1. 起全栈（网关 + redis + ollama + prometheus + grafana）
docker compose up -d

# 2. 拉取模型（首次需要，约 1GB）
docker compose exec ollama ollama pull qwen2.5:1.5b

# 3. 发一条非流式请求（租户 Key 见 config/config.docker.yaml）
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-llmgate-zhangsan-key" \
  -H "Content-Type: application/json" \
  -d '{"model": "qwen2.5:1.5b", "messages": [{"role": "user", "content": "你好，介绍一下你自己"}]}'

# 4. 流式请求（SSE）
curl -N http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-llmgate-zhangsan-key" \
  -H "Content-Type: application/json" \
  -d '{"model": "qwen2.5:1.5b", "messages": [{"role": "user", "content": "讲个笑话"}], "stream": true}'
```

也可以直接用 OpenAI SDK，把 `base_url` 指向网关即可：

```python
from openai import OpenAI
client = OpenAI(base_url="http://localhost:8080/v1", api_key="sk-llmgate-zhangsan-key")
resp = client.chat.completions.create(model="qwen2.5:1.5b",
                                      messages=[{"role": "user", "content": "hello"}])
```

- Grafana 看板：`http://localhost:3000`（默认 admin/admin，预置 QPS/延迟/Token/限流/缓存面板）
- 原生运行（不用容器跑网关）：`cp config/config.example.yaml config/config.yaml`，按需修改后 `go run ./src`

## 📈 性能（真实压测）

用可控假上游（`cmd/benchmock`）剥离模型推理延迟，测量**网关自身**的转发性能，wrk 同机压测（i7-13700KF / 24 核 / WSL2，详见 [BENCHMARK.md](BENCHMARK.md)）：

| 并发 | QPS | P50 | P99 | 错误 |
|---:|---:|---:|---:|---:|
| 1 | 1,345 | 717 µs | 1.13 ms | 0 |
| 10 | 6,335 | 1.22 ms | 2.16 ms | 0 |
| 50 | 12,919 | 3.60 ms | 6.23 ms | 0 |
| **100** | **16,099** | 5.84 ms | **9.25 ms** | 0 |
| 200 | 16,953 | 11.68 ms | 15.88 ms | 0 |

- **~17k QPS、P99 < 16ms、5 档累计 157 万请求零错误**；1 vs 3 上游对照峰值仅 +4.3%，证明瓶颈在网关/主机侧而非上游。
- **故障隔离**：3 上游中 1 个持续注入 50% 故障时，熔断自动摘除坏节点，**请求成功率 100%**，吞吐/延迟与健康集群持平。

## 🧩 设计取舍

- **Go 而非 C++**：网关是重 I/O、重并发的场景，goroutine + net/http 生态更契合；C++ 数据面的性能优势在本项目量级下不构成瓶颈。
- **内存令牌桶 vs Redis 令牌桶**：单实例内存版足够，多实例部署必须 Redis + Lua 保证原子性；抽象成 `Limiter` 接口双实现，按部署形态切换。
- **限流 fail-open / 计费 fail-closed**：Redis 抖动时限流放行（可用性优先），余额不足直接 402（一致性优先）——两种故障的业务代价不同，策略就不该相同。
- **精确缓存做、语义缓存主动不做（基于成本模型）**：精确缓存命中路径只有 `hash + Redis GET`，零推理成本把完全重复的请求变秒回，稳赚。语义缓存则要"先付一次 embedding 推理 + 向量检索"换"省一次生成"，只有生成成本 ≫ 嵌入成本时才划算；本项目上游是本地小模型，生成近乎免费，embedding 还占同样的 GPU，并背上误命中风险——净收益为负。触发条件与方案已写入 Roadmap。
- **用量落库异步 + 可丢弃**：观测/账本不能反噬主链路，写盘失败只告警不阻断请求。
- **协议转换收敛在 Provider 层**：新增厂商只需实现一个 `Provider` 接口（Chat/ChatStream），路由、限流、熔断等核心逻辑零改动。
- **流式故障转移只在首字节前**：SSE 一旦向客户端写出第一个 chunk，就无法再换上游重放，此时只能断开——用明确的边界换取实现的可控性。

## 📁 项目结构

```
src/
├── main.go         # 组装根：配置加载、依赖注入、优雅关闭
├── config/         # YAML 配置加载、默认值与校验
├── middleware/     # Recovery / TraceID / Prometheus 指标中间件
├── auth/           # 多租户鉴权，租户元数据存 Redis
├── gateway/        # 请求编排：限流 → 缓存 → 路由 → 转发 → 计量
├── router/         # 模型 → 上游节点组的路由表
├── lb/             # 负载均衡（轮询/加权轮询/最少连接）+ 三态熔断器
├── health/         # 定时健康检查，驱动节点存活状态
├── provider/       # 上游协议适配层：OpenAI / Ollama（含 SSE⇄NDJSON 互转）
├── openai/         # OpenAI 协议数据结构定义
├── keypool/        # 上游 Key 池，429 冷却轮换
├── ratelimit/      # RPM/TPM 令牌桶（内存 + Redis/Lua 双实现）、余额、用量 Recorder
├── cache/          # 精确响应缓存（TTL 抖动）
├── metrics/        # Prometheus 指标注册
├── logger/         # zerolog 封装
└── utils/          # 工具函数
cmd/benchmock/      # OpenAI 兼容假上游（压测用，支持注入故障率）
bench/post.lua      # wrk 压测脚本
deploy/             # Prometheus / Grafana 预置配置与看板
```

## 🗺️ Roadmap

- [ ] **语义缓存**：当上游切换为按量付费的远程 API 或大而慢的模型、且流量高重复措辞多变时启用；方案：Redis Stack HNSW 向量索引，相似度阈值从 0.95 起离线调优（宁可不命中也别答错）
- [ ] **请求前 TPM 预扣**：tiktoken 精确预估 prompt tokens + `Reserve/Settle` 原子机制，消除并发穿透
- [ ] 一致性哈希负载均衡
- [ ] 多实例部署的控制面一致性（配置热更新、租户管理 API）
- [ ] K8s 部署（Helm chart）

## 📄 License

[MIT](LICENSE)
