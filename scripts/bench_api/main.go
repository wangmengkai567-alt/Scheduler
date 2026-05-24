// Package main 提供 API 接口性能压测
// 运行方式：go run scripts/bench_api.go
// Windows 友好，无需 wrk
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// 压测配置
const (
	BaseURL           = "http://localhost:8080"
	Duration          = 10 * time.Second
	APIWarmupDuration = 2 * time.Second
	Concurrency       = 10 // 并发数
)

// 统计数据
type APIStats struct {
	mu        sync.Mutex
	latencies []time.Duration
	success   int64
	failed    int64
}

func NewAPIStats() *APIStats {
	return &APIStats{
		latencies: make([]time.Duration, 0, 10000),
	}
}

func (s *APIStats) Record(latency time.Duration, success bool) {
	s.mu.Lock()
	s.latencies = append(s.latencies, latency)
	s.mu.Unlock()
	if success {
		atomic.AddInt64(&s.success, 1)
	} else {
		atomic.AddInt64(&s.failed, 1)
	}
}

func (s *APIStats) Report() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.latencies) == 0 {
		fmt.Println("⚠️ 没有收集到数据")
		return
	}

	sort.Slice(s.latencies, func(i, j int) bool {
		return s.latencies[i] < s.latencies[j]
	})

	total := len(s.latencies)
	avg := time.Duration(0)
	for _, l := range s.latencies {
		avg += l
	}
	avg = time.Duration(int64(avg) / int64(total))

	p50 := s.latencies[total*50/100]
	p90 := s.latencies[total*90/100]
	p95 := s.latencies[total*95/100]
	p99 := s.latencies[total*99/100]

	qps := float64(total) / Duration.Seconds()

	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("📊 API 接口性能压测报告")
	fmt.Println("============================================================")
	fmt.Printf("🔧 配置: 并发=%d, Duration=%v\n", Concurrency, Duration)
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("📈 吞吐量:\n")
	fmt.Printf("   QPS:        %.0f req/s\n", qps)
	fmt.Printf("   总请求数:   %d\n", total)
	fmt.Printf("   成功:       %d\n", atomic.LoadInt64(&s.success))
	fmt.Printf("   失败:       %d\n", atomic.LoadInt64(&s.failed))
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("⏱️  延迟统计:\n")
	fmt.Printf("   平均:       %v\n", avg)
	fmt.Printf("   P50:        %v\n", p50)
	fmt.Printf("   P90:        %v\n", p90)
	fmt.Printf("   P95:        %v\n", p95)
	fmt.Printf("   P99:        %v\n", p99)
	fmt.Printf("   最大:       %v\n", s.latencies[total-1])
	fmt.Println("============================================================")

	fmt.Println()
	fmt.Println("📝 简历格式:")
	fmt.Printf("   触发接口 P99 延迟 %.2fms，QPS %.0f\n",
		float64(p99.Microseconds())/1000, qps)
}

func main() {
	fmt.Println("🚀 API 接口性能压测")
	fmt.Printf("配置: 并发 %d, Duration %v\n", Concurrency, Duration)
	fmt.Println()

	// 检查服务状态
	fmt.Println("🔍 检查服务状态...")
	resp, err := http.Get(BaseURL + "/health")
	if err != nil || resp.StatusCode != 200 {
		fmt.Println("❌ 服务未启动，请先运行: go run cmd/server/main.go")
		return
	}
	resp.Body.Close()
	fmt.Println("✅ 服务已就绪")
	fmt.Println()

	// 创建测试任务
	fmt.Println("📝 创建测试任务...")
	taskResp, err := http.Post(
		BaseURL+"/api/v1/tasks",
		"application/json",
		bytes.NewBufferString(`{"name":"bench-task","cron_expr":"0 0 * * * *","payload":"{}"}`),
	)
	if err != nil {
		fmt.Println("❌ 创建任务失败:", err)
		return
	}

	var taskResult struct {
		Data struct {
			ID uint `json:"id"`
		} `json:"data"`
	}
	body, _ := io.ReadAll(taskResp.Body)
	taskResp.Body.Close()
	json.Unmarshal(body, &taskResult)
	taskID := taskResult.Data.ID

	if taskID == 0 {
		fmt.Println("❌ 创建任务失败，未获取到任务ID")
		fmt.Println("响应:", string(body))
		return
	}
	fmt.Printf("✅ 测试任务已创建, ID: %d\n", taskID)
	fmt.Println()

	// 预热
	fmt.Printf("🔥 预热中 (%v)...\n", APIWarmupDuration)
	warmupStats := NewAPIStats()
	var wg sync.WaitGroup
	for i := 0; i < Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			timeout := time.After(APIWarmupDuration)
			for {
				select {
				case <-timeout:
					return
				default:
					start := time.Now()
					resp, err := http.Post(
						fmt.Sprintf("%s/api/v1/tasks/%d/trigger", BaseURL, taskID),
						"application/json",
						bytes.NewBufferString(`{}`),
					)
					latency := time.Since(start)
					if err != nil {
						warmupStats.Record(latency, false)
						continue
					}
					resp.Body.Close()
					warmupStats.Record(latency, resp.StatusCode < 500)
				}
			}
		}()
	}
	wg.Wait()
	fmt.Println("✅ 预热完成")
	fmt.Println()

	// 正式压测
	fmt.Printf("⚡ 压测中 (%v)...\n", Duration)
	stats := NewAPIStats()

	for i := 0; i < Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			timeout := time.After(Duration)
			for {
				select {
				case <-timeout:
					return
				default:
					start := time.Now()
					resp, err := http.Post(
						fmt.Sprintf("%s/api/v1/tasks/%d/trigger", BaseURL, taskID),
						"application/json",
						bytes.NewBufferString(`{}`),
					)
					latency := time.Since(start)
					if err != nil {
						stats.Record(latency, false)
						continue
					}
					resp.Body.Close()
					stats.Record(latency, resp.StatusCode < 500)
				}
			}
		}()
	}
	wg.Wait()

	// 输出报告
	stats.Report()

	// 清理测试任务
	fmt.Println()
	fmt.Println("🧹 清理测试数据...")
	req, _ := http.NewRequest("DELETE", fmt.Sprintf("%s/api/v1/tasks/%d", BaseURL, taskID), nil)
	http.DefaultClient.Do(req)
	fmt.Println("✅ 清理完成")
}
