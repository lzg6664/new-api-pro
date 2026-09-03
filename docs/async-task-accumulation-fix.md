# 异步生图任务「消息积累」根因与修复（2026-09）

## 现象

banana2/banana_pro、tiapi 渠道生图随进程运行时间变慢，重启 new-api-pro 后恢复。

## 根因

banana/tiapi 均为 `sync_mode: true`（120 次 × 5s）：每个生图请求占住一个 handler 协程同步轮询最长 600 秒。四个只增不减的累积点叠加：

1. **被放弃的轮询器**：轮询循环不感知客户端断开（裸 `time.Sleep`，无 ctx 检查），且上游查询无超时（`RELAY_TIMEOUT=0`）。Java 侧 WebClient 超时恰好也是 600s（`ImageGenerationProperties.NewApiConfig.timeout=600`），凡接近超时被掐断的请求都会在网关留下一个盲跑的轮询协程，攥着 MB 级 b64 请求体继续跑满预算；上游挂起时协程永久卡死。重启即全部释放——即「重启释放了消息」。
2. **每 5 秒/任务的固定开销**：每次轮询做一次不走缓存的渠道全行 DB 查询 + 2~3 行 `[sync-poll]` 日志，全部挤过单一全局锁的同步日志写入器；在途任务越多，所有请求排队越久。
3. **15s 后台 TaskPollingLoop 双轮询**：自定义异步任务落库 `Platform="1"`（渠道类型号）被映射到 sora 适配器，用 banana/tiapi 不存在的 `/v1/videos/{id}` 端点查询 → 永远报错 → 任务永不到终态 → 每 15s 重扫 + 每任务串行睡 1s，僵尸留 24h，且会误杀在途任务。
4. **tasks 表膨胀**：每行存完整原始请求体（含 b64 参考图，MB 级）且永不删除（此项重启不可恢复）。

## 修复内容

| 修复 | 位置 | 说明 |
|---|---|---|
| A 轮询生命周期 | `relay/async_task/poller.go` `handler.go` | `PollSynchronously` 接收 ctx（`c.Request.Context()`），sleep 改为 ctx 感知 select；单次上游查询 15s 硬超时（`NewRequestWithContext`）；客户端断开时**脱尾后台收尾**（复制 info、丢弃 Request 载荷、后台跑到真实终态、不写响应、不误判 FAILURE）；后台轮询路径同样不再持有请求载荷 |
| B 平台双轮询 | `constant/task.go` `handler.go` `service/task_polling.go` | 新增 `TaskPlatformAsyncTask="async"`，自定义异步任务落库改用之；15s 循环跳过该平台 |
| C 日志降噪 | `poller.go` `relay_trace.go` `api_request.go` `relay-openai.go` `task_polling.go` `pipeline.go` | 尝试级 `[sync-poll]`、`[RELAY-*]` 全量转发日志、COS 过程日志统一 `DEBUG=true` 才输出；保留每任务一行终态日志与真实错误；删除裸 `fmt.Println` 响应体打印 |
| D tasks 表瘦身 | `handler.go` `service/task_cleanup.go`(新) `constant/env.go` `main.go` | 不再存 `RawSubmitBody`；新增每小时清理循环物理删除超过 `TASK_RETENTION_DAYS`（默认 7，0=禁用）的终态任务 |
| E 杂项 | `poller.go` `handler.go` | 渠道可用性检查改走 `CacheGetChannel`（内存缓存，未启用时自动回退 DB）；`asyncTaskConsumeLogIDs` 终态处理后删除条目 |

## 部署清单

1. 部署新版本后，**执行一次**存量僵尸清理 SQL（三库通用；`raw_submit_body` 只有旧自定义异步任务的 data 里才有，不影响真 sora 视频任务；`finish_time=1` 使其立即满足保留期清理条件，连同 MB 级旧 body 一并被物理删除）：

   ```sql
   UPDATE tasks SET status='FAILURE', progress='100%', finish_time=1,
     fail_reason='legacy custom async task cleanup'
   WHERE platform='1' AND status NOT IN ('SUCCESS','FAILURE')
     AND data LIKE '%raw_submit_body%';
   ```

2. 建议环境变量：

   | Env | 值 | 说明 |
   |---|---|---|
   | `MEMORY_CACHE_ENABLED` | `true` | 渠道检查零 DB（未设时行为与旧版相同） |
   | `TASK_TIMEOUT_MINUTES` | `60`（可选，默认 1440） | 崩溃遗留 async 任务更快被清扫 |
   | `ENABLE_PPROF` | `true`（验证期） | `:8005` 观测 goroutine |
   | `TASK_RETENTION_DAYS` | `7`（默认，0=禁用） | 终态任务保留天数 |

3. Java 侧（可选，非阻塞）：`newapi.timeout=600` 与网关轮询预算同值零余量，建议提到 630s，或把渠道 `max_poll_attempts` 降到 110。

## 验证

- 掐断测试：`curl --max-time 20` 发起生图，`curl -s "http://127.0.0.1:8005/debug/pprof/goroutine?debug=1" | grep -c PollSynchronously` —— handler 协程应秒级退出（至多留一个有界后台收尾协程）。
- 全天流量下早晚对比 goroutine 总数，应平稳。
- 日志不再出现对 banana/tiapi 的 `/v1/videos/` 请求；`DEBUG` 未开时无 `[sync-poll]`（终态行除外）/`[RELAY-*]` 行。
- `SELECT platform,status,count(*) FROM tasks WHERE progress!='100%' GROUP BY 1,2` 不再有 `platform='1'` 增长；新任务均为 `async`。
