# GTask Scheduler

一个轻量级的 Go 任务调度器，用于简历展示的后端实习项目。

## 项目特点

- ✅ Go 并发编程（goroutine、channel、sync 包）
- ✅ 工程规范（接口抽象、错误处理、结构化日志）
- ✅ 中间件运用（MySQL、Redis、分布式锁）
- ✅ 可测试性设计（接口 mock、单元测试覆盖率）

## 技术栈

| 组件     | 技术选型                        |
| -------- | ------------------------------- |
| Web 框架 | Gin                             |
| ORM      | GORM + MySQL 8.0                |
| 缓存/锁  | go-redis/v9 + Redis 7           |
| 定时调度 | robfig/cron/v3（支持秒级）      |
| 日志     | uber-go/zap                     |
| 测试     | 标准库 testing + testify/assert |

## 项目结构

```
gtask-scheduler/
├── cmd/server/main.go           # 程序入口，依赖注入
├── internal/
│   ├── api/
│   │   ├── handler/task.go      # HTTP 处理器
│   │   └── middleware/logger.go # 请求日志中间件
│   ├── config/config.go         # 配置加载
│   ├── model/task.go            # 数据模型
│   ├── scheduler/scheduler.go   # 调度器核心
│   ├── store/
│   │   ├── mysql/task.go        # GORM CRUD
│   │   └── redis/lock.go        # 分布式锁
│   └── worker/
│       ├── handler.go           # JobHandler 接口
│       ├── pool.go              # Worker Pool
│       └── pool_test.go         # 单测
├── pkg/
│   ├── errors/errors.go         # 错误码
│   ├── logger/logger.go         # Zap 封装
│   └── response/response.go     # 统一响应
├── go.mod
└── README.md
```

## 快速开始

### 环境要求

- Go 1.21+
- MySQL 8.0+
- Redis 7+（可选，用于分布式锁）

### 安装依赖

```bash
go mod tidy
```

### 配置环境变量

```bash
# 服务配置
export SERVER_HOST=0.0.0.0
export SERVER_PORT=8080
export GIN_MODE=debug

# 数据库配置
export DB_HOST=localhost
export DB_PORT=3306
export DB_USER=root
export DB_PASSWORD=your_password
export DB_NAME=gtask

# Redis 配置（可选）
export REDIS_HOST=localhost
export REDIS_PORT=6379
export REDIS_PASSWORD=

# 日志配置
export LOG_LEVEL=info
export LOG_ENCODING=console
```

### 创建数据库

```sql
CREATE DATABASE gtask CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

### 启动服务

```bash
go run cmd/server/main.go
```

### API 示例

```bash
# 创建任务
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test-task",
    "cron_expr": "*/10 * * * * *",
    "payload": "{\"action\": \"ping\"}",
    "max_retries": 3
  }'

# 查询任务列表
curl http://localhost:8080/api/v1/tasks

# 查询单个任务
curl http://localhost:8080/api/v1/tasks/1

# 更新任务
curl -X PUT http://localhost:8080/api/v1/tasks/1 \
  -H "Content-Type: application/json" \
  -d '{"name": "updated-task"}'

# 删除任务
curl -X DELETE http://localhost:8080/api/v1/tasks/1

# 健康检查
curl http://localhost:8080/health
```

## 核心设计

### 1. Worker Pool

固定数量的 worker goroutine 消费任务队列，控制并发度：

```go
pool := worker.NewPool(10, 1000, handler)
pool.Start()
pool.Submit(job)
pool.Stop()
```

**设计要点：**

- 通过 channel 实现任务分发，天然并发安全
- atomic.Bool 保证只启动/停止一次
- panic recovery 防止单个任务失败导致 worker 退出

### 2. 分布式锁

基于 Redis SET NX EX 实现分布式锁：

```go
lock := redis.NewLock(client, "task:123", "instance-1",
    redis.WithTTL(60*time.Second),
    redis.WithRetries(3),
)
lock.Acquire(ctx)
defer lock.Release(ctx)
```

**设计要点：**

- Lua 脚本保证原子性
- 只释放自己持有的锁，避免误删
- 支持自动续期（Refresh）

### 3. 错误处理

统一的错误码和类型：

```go
// 数据库错误转换为应用错误
if errors.Is(err, gorm.ErrRecordNotFound) {
    return nil, errors.NewNotFound("task", id)
}

// HTTP 层统一处理
response.Error(c, err)
```

## 运行测试

```bash
# 运行所有测试
go test ./internal/... -v -cover

# 查看覆盖率
go test -cover ./internal/...
```

## 性能压测

### 压测环境

- 操作系统：macOS / Linux
- Go 版本：1.21+
- 工具：wrk（HTTP 压测）、Go 标准库（Worker Pool 压测）

### 1. Worker Pool 吞吐量测试

测试 Worker Pool 的并发处理能力：

```bash
go run scripts/bench_pool.go
```

**实际结果：**

```
============================================================
📊 Worker Pool 性能压测报告
============================================================
🔧 配置: Worker=10, QueueSize=1000, Duration=10s
------------------------------------------------------------
📈 吞吐量:
   QPS:        1695 req/s
   总任务数:   15949
   成功:       16950
   失败:       0
------------------------------------------------------------
⏱️  延迟统计:
   平均:       625.439µs
   P50:        608.4µs
   P90:        730.2µs
   P95:        744.9µs
   P99:        802.5µs
   最大:       1.728ms
============================================================

📝 简历格式:
   Worker Pool 在并发度 10 下，任务吞吐量达到 1695 req/s，P99 延迟 0.80ms
```

| 指标     | 数值         |
| -------- | ------------ |
| QPS      | `1695` req/s |
| P99 延迟 | `0.80` ms    |
| 平均延迟 | `0.63` ms    |

### 2. API 接口性能测试

测试 API 接口的并发处理能力：

```bash
# 首先启动服务
go run cmd/server/main.go

# 新终端运行压测脚本
go run scripts/bench_api.go
```

**实际结果：**

```
============================================================
📊 API 接口性能压测报告
============================================================
🔧 配置: 并发=10, Duration=10s
------------------------------------------------------------
� 吞吐量:
   QPS:        1532 req/s
   总请求数:   15324
   成功:       15324
   失败:       0
------------------------------------------------------------
⏱️  延迟统计:
   平均:       6.48948ms
   P50:        6.1431ms
   P90:        10.1775ms
   P95:        11.5489ms
   P99:        14.1545ms
   最大:       22.1062ms
============================================================

📝 简历格式:
   触发接口 P99 延迟 14.15ms，QPS 1532
```

| 接口                           | QPS          | P99 延迟   |
| ------------------------------ | ------------ | ---------- |
| POST /api/v1/tasks/:id/trigger | `1532` req/s | `14.15` ms |

### 3. Redis 分布式锁争抢验证

验证分布式锁在并发场景下的正确性：

```bash
go run scripts/bench_lock.go
```

**实际结果：**

```
============================================================
📊 Redis 分布式锁验证报告
============================================================
🔧 配置:
   并发数:     10 goroutines
   测试轮数:   100
   总尝试次数: 1000
------------------------------------------------------------
📈 结果:
   正确执行:   100 次 (仅 1 个 goroutine 执行)
   重复执行:   0 次 (超过 1 个 goroutine 执行)
   成功率:     100.00%
============================================================

📊 执行次数分布:
   执行 1 次: 100 轮

============================================================
✅ 验证通过！

📝 简历格式:
   并发 10 次触发同一任务，Redis 分布式锁保证仅执行 1 次，零重复执行
============================================================
```

### 压测结果汇总

#### Worker Pool 性能

- Worker Pool 在并发度 10 下，任务吞吐量达到 `1695` req/s，P99 延迟 `0.80`ms

#### API 接口性能

- 触发接口 P99 延迟 `14.15`ms，QPS `1532`

#### 分布式锁验证

- 并发 10 次触发同一任务，Redis 分布式锁保证仅执行 1 次，零重复执行

## 测试覆盖率

| 模块        | 覆盖率 |
| ----------- | ------ |
| handler     | 80.9%  |
| middleware  | 97.9%  |
| config      | 100%   |
| scheduler   | 73.3%  |
| store/mysql | 81.4%  |
| store/redis | 92.2%  |
| worker      | 92.3%  |
