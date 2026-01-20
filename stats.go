// stats.go
package main

import (
	"fmt"
	"sync"
	"time"
)

type Statistics struct {
    mu           sync.Mutex
    requestCount int
    totalTime    time.Duration
    cacheHits    int
    requests     []RequestInfo // Добавим историю запросов для анализа
}

type RequestInfo struct {
    Timestamp time.Time
    Duration  time.Duration
    Type      string // "query", "command", "llm"
}

// Обновим RecordRequest:
func (s *Statistics) RecordRequest(duration time.Duration, reqType string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    s.requestCount++
    s.totalTime += duration
    
    // Сохраняем последние 100 запросов для анализа
    s.requests = append(s.requests, RequestInfo{
        Timestamp: time.Now(),
        Duration:  duration,
        Type:      reqType,
    })
    
    // Ограничиваем размер истории
    if len(s.requests) > 100 {
        s.requests = s.requests[1:]
    }
}

// Обновим GetStats:
func (s *Statistics) GetStats() map[string]interface{} {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    avgTime := time.Duration(0)
    if s.requestCount > 0 {
        avgTime = s.totalTime / time.Duration(s.requestCount)
    }
    
    // Рассчитываем статистику за последний час
    hourAgo := time.Now().Add(-1 * time.Hour)
    recentCount := 0
    var recentTotal time.Duration
    
    for _, req := range s.requests {
        if req.Timestamp.After(hourAgo) {
            recentCount++
            recentTotal += req.Duration
        }
    }
    
    recentAvg := time.Duration(0)
    if recentCount > 0 {
        recentAvg = recentTotal / time.Duration(recentCount)
    }
    
    return map[string]interface{}{
        "requestCount":           s.requestCount,
        "totalTime":              s.totalTime.String(),
        "avgRequestTime":         avgTime.String(),
        "avgRequestTimeMs":       avgTime.Milliseconds(),
        "cacheHits":              s.cacheHits,
        "recentHourRequests":     recentCount,
        "recentAvgRequestTime":   recentAvg.String(),
        "recentAvgRequestTimeMs": recentAvg.Milliseconds(),
        "requestsPerMinute":      float64(recentCount) / 60.0,
    }
}

func NewStatistics() *Statistics {
	return &Statistics{}
}

func (s *Statistics) RecordCacheHit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cacheHits++
}

func (s *Statistics) Display() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	avgTime := time.Duration(0)
	if s.requestCount > 0 {
		avgTime = s.totalTime / time.Duration(s.requestCount)
	}
	
	fmt.Printf("📊 Запросов: %d, Среднее время: %v\n", 
		s.requestCount, avgTime)
}

// Reset сбрасывает статистику
func (s *Statistics) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.requestCount = 0
	s.totalTime = 0
	s.cacheHits = 0
}