# llmgate 性能压测报告（BENCHMARK）

> **目标**：用可控的假上游（bench mock）剥离真实模型推理延迟，单独测量 **网关自身** 的转发性能与容错能力。
>
> **结论先行（两条可写简历的硬数据）**：
> 1. **吞吐**：网关自身达 **~17k QPS、P99 < 10ms、零错误**（并发 100 时 ~16k QPS / P99 9.25ms；并发 200 进入平台期）。1 vs 3 上游对照（峰值仅 +4.3%）证明瓶颈在网关/主机侧而非上游。
> 2. **容错**：3 上游中 1 个持续注入 50% 故障时，网关经熔断自动摘除坏节点，**请求成功率 100%（零非 2xx）**，吞吐/延迟与健康集群持平——**故障隔离几乎零开销**。
>
> **口径说明**：压测客户端与网关同机、共享 24 核 CPU，上述为**保守下限**；分机部署消除客户端 CPU 争抢后上限更高。

---

## 1. 测试环境

| 项 | 值 |
|---|---|
| CPU | 13th Gen Intel Core i7-13700KF（24 逻辑核） |
| 内存 | 16 GiB |
| OS / 运行时 | WSL2 (Ubuntu) / linux-amd64 |
| Go 版本 | go1.27.1 |
| 网关运行方式 | **原生 `go run ./src`**（非容器，省掉 veth/bridge/NAT 网络开销） |
| 上游 | **benchmock**（OpenAI 兼容假上游，静态返回带 usage）：吞吐测试用 1 个（`:11499`），对照/容错用 3 个（`:11499/:11500/:11501`） |
| Redis | Docker 容器，`localhost:6379`（限流/余额后端） |
| 缓存 | **关闭**（`cache.enable: false`，测纯转发路径） |
| 限流 | 拉满（RPM 100 万 / TPM 1 亿），避免成为瓶颈 |
| 压测客户端 | wrk（**与网关、mock 同机**，共享 CPU） |
| 压测脚本 | `bench/post.lua`（POST 非流式，每请求随机 content 防缓存） |
| 配置文件 | 单上游 `config/config.bench.native.yaml`；多上游 `config/config.bench.native.multi.yaml` |

---

## 2. 测试方法

- **并发梯度**：`c = 1 / 10 / 50 / 100 / 200`，每档 `-d30s --latency`；客户端线程 `-t8`，**c=1 档特例**用 `-t1 -c1`（wrk 要求连接数 ≥ 线程数）。
- **记录指标**：`Requests/sec`（QPS）、`Latency P50/P90/P99`、`Non-2xx`（错误数）；容错场景另取网关 `/metrics` 的 `llmgate_requests_total{status}` 与 `llmgate_failover_total`。
- **命令模板**：
  ```bash
  wrk -t8 -c$c -d30s --latency -s bench/post.lua http://localhost:8080/v1/chat/completions
  wrk -t1 -c1   -d30s --latency -s bench/post.lua http://localhost:8080/v1/chat/completions   # c=1
  ```

---

## 3. 结果一：网关吞吐与延迟

### 3.1 并发梯度（单上游）

| 并发 c | QPS | P50 | P90 | P99 | Avg | Max | 错误 |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | 1,345 | 717 µs | 841 µs | 1.13 ms | 736 µs | 3.47 ms | 0 |
| 10 | 6,335 | 1.22 ms | 1.49 ms | 2.16 ms | 1.26 ms | 7.84 ms | 0 |
| 50 | 12,919 | 3.60 ms | 4.80 ms | 6.23 ms | 3.74 ms | 84.29 ms | 0 |
| **100** | **16,099** | 5.84 ms | 7.44 ms | **9.25 ms** | 5.96 ms | 40.25 ms | 0 |
| 200 | 16,953 | 11.68 ms | 13.61 ms | 15.88 ms | 11.81 ms | 70.91 ms | 0 |

**分析**：
1. **拐点在 c ≈ 100**：c100→c200 连接翻倍，QPS 仅 +5%（16,099→16,953），P99 却翻倍（9.25→15.88ms）——网关进入吞吐饱和区，再加并发只是内部排队。
2. **全程零错误**：5 档累计 ~157 万请求，Non-2xx = 0。
3. **数据自洽（Little's Law）**：`QPS × 平均延迟 ≈ 连接数`——c=100 `16099×5.96ms≈96`✓、c=200 `16953×11.81ms≈200`✓、c=1 `1345×0.736ms≈1`✓。
4. **网关自身开销为毫秒级**：P99 已含"网关→mock→回网关"整条 loopback 往返，纯网关处理开销更低。Max 偶发 70~84ms 为 GC/调度抖动，以 P99 为准。

### 3.2 多上游对照（验证瓶颈归属）

将上游从 1 个扩到 **3 个**（等权加权轮询，均不注错），同一梯度重跑：

| 并发 c | 单 mock QPS | 3 mock QPS | 变化 | 单 mock P99 | 3 mock P99 |
|---:|---:|---:|---:|---:|---:|
| 1 | 1,345 | 1,393 | +3.6% | 1.13 ms | 1.11 ms |
| 10 | 6,335 | 6,289 | −0.7% | 2.16 ms | 1.84 ms |
| 50 | 12,919 | 12,795 | −1.0% | 6.23 ms | 6.35 ms |
| 100 | 16,099 | 16,587 | +3.0% | 9.25 ms | 9.11 ms |
| 200 | 16,953 | **17,683** | **+4.3%** | 15.88 ms | 15.04 ms |

**结论：瓶颈不在上游。** 三倍上游仅换来峰值 +4.3%，各档波动全在 ±4% 噪声内 → 坐实限制来自 **网关自身处理 + 同机 CPU 争抢**，~17k QPS 即本机真实上限。附带收益：多上游削尾延迟（c=50 Max 84.29→32.65ms）。

---

## 4. 结果二：上游故障隔离（failover + 熔断）

- **配置**：`config.bench.native.multi.yaml`（3 上游等权），mock-c 以 `-error-rate 0.5` 持续注入 500；非流式，`wrk -t8 -c50 -d30s`。
- **结果**：

  | 指标 | 值 |
  |---|---|
  | 客户端请求数 | 392,969（wrk） |
  | 成功率 | **100%**（`requests_total{status="200"}`=393,011，无任何非 2xx） |
  | wrk Non-2xx | **0** |
  | QPS | 13,095（vs 健康 3-mock 基线 12,795，持平） |
  | P99 | 6.29 ms（vs 基线 6.35 ms，持平） |
  | `failover_total` | 38 |

- **关键解读：failover 仅 38 次是"熔断生效"的证据，而非 failover 未工作。**
  - 路由 `Pick()` 仅选 `Healthy() && CB.Allow()` 的节点；熔断器参数 threshold=3、cooldown=10s、probeInterval=1s。
  - benchmock 错误注入只在 `/chat/completions`，健康检查打 `/models` 恒返回 200 → **健康检查不会摘除 c，摘除完全由熔断器完成**（贴近真实：上游 API 报错时健康端点往往仍存活）。
  - mock-c 连续 3 次失败即熔断打开，随后 10s 冷却期内被 `Pick()` 完全排除 → 几乎无流量打到 c → 无需反复 failover，故 30s 内仅 38 次（发生在熔断打开前及少量半开探测窗口）。
  - **对照**：若无熔断器，约 1/3 流量落到 c、其中半数失败 → failover 会高达 ~6.5 万次且每次多一跳重试开销。熔断把"每请求重试"优化成"直接绕开坏节点"。

---

## 5. 数据可信度 Caveat（解读前必读）

1. **同机争抢 CPU → 数字是保守下限。** wrk（客户端）、网关、mock 同机共享 24 核，客户端自身吃大量 CPU。要测网关真实天花板，需将压测客户端与被测网关**分机部署**。
2. **瓶颈已确认在网关/主机侧，非上游**（见 3.2）。~17k QPS 是本机 wrk+网关+mock 共享 CPU 下的上限。
3. **假上游剥离了推理延迟**：本报告测的是"网关转发开销"，不代表任何真实模型的端到端时延；真实上游下总时延由模型主导（秒级），不在本报告口径内。

---

## 6. 已评估但不纳入的场景（附理由）

以下场景经评估后**不作为简历量化数据**，原因是它们测的是"人为注入的参数"而非网关能力，或属功能正确性而非性能：

| 场景 | 不纳入的理由 |
|---|---|
| **C 流式 TTFT** | mock 的 `-first-delay 3ms` 是人为注入的假延迟，测出的 TTFT 主要反映该注入值而非网关能力（循环论证）。且 TTFT 直方图最小桶 50ms，3.8ms 级样本全落首桶，P50/P99 为桶内均匀插值假象；仅 `Avg=3.8ms`（sum/count）可信，用于验证流式埋点链路正常。 |
| **E 缓存命中率** | 本网关为 L1 **精确缓存**（key = sha256(model+messages+temperature+max_tokens)），命中率完全由压测脚本的请求重复度决定，是"测自己注入的重复率"的循环论证。且精确缓存在真实 LLM 流量（prompt 近乎唯一）中命中率天然很低，不宜作亮点。 |
| **D 限流精度** | 属**规格符合性验证**（正确性数字），非性能头条；且需并发突发 + 每条 body 唯一 + 每轮重置令牌桶，测量成本较高。限流功能本身已具备（令牌桶容量=RPM、回填=RPM/60，可正确返回 429 并打点 `rate_limited_total`），此处不纳入量化简历数据。 |

> 方法论收获：**直方图桶设计必须匹配被测延迟量级**；**量化指标应反映网关能力，而非测试脚本注入的参数**——否则数字不可防守。

---

## 7. 复现步骤

```bash
# 0) 确保 Docker 里 llmgate/benchmock 已停，仅 redis 在跑（避免 :8080 冲突）
docker compose -f docker-compose.yaml -f docker-compose.bench.yaml stop llmgate benchmock-a benchmock-b benchmock-c ollama

# --- 结果一（3.1 单上游吞吐梯度）---
# 终端 A：单个假上游
go run ./cmd/benchmock -addr :11499 -chunks 20 -first-delay 3ms -chunk-interval 2ms
# 终端 B：网关
LLMGATE_CONFIG=config/config.bench.native.yaml go run ./src
# 终端 C：冒烟
curl -s http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-llmgate-bench" -H "Content-Type: application/json" \
  -d '{"model":"bench-gpt","stream":false,"messages":[{"role":"user","content":"hi"}]}'
# 终端 C：并发梯度
wrk -t1 -c1 -d30s --latency -s bench/post.lua http://localhost:8080/v1/chat/completions
for c in 10 50 100 200; do
  echo "==================== c=$c ===================="
  wrk -t8 -c$c -d30s --latency -s bench/post.lua http://localhost:8080/v1/chat/completions
done

# --- 结果一（3.2 多上游对照）---
# 起 3 个干净 mock（:11499/:11500/:11501），网关换多上游配置，重跑同一梯度
LLMGATE_CONFIG=config/config.bench.native.multi.yaml go run ./src

# --- 结果二（第 4 节 故障隔离）---
# mock-c 注错 50%，其余干净；网关用多上游配置【全新启动使计数器归零】
go run ./cmd/benchmock -addr :11501 -error-rate 0.5 -chunks 20 -first-delay 3ms -chunk-interval 2ms
wrk -t8 -c50 -d30s --latency -s bench/post.lua http://localhost:8080/v1/chat/completions
curl -s localhost:8080/metrics | grep -E "llmgate_failover_total|llmgate_requests_total"
```
