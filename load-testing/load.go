package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
	"log"
)

type Request struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	MaxTokens int `json:"max_tokens"`
}

func worker(id int, concurrencyChan chan struct{}, wg *sync.WaitGroup, stats chan time.Duration) {
	defer wg.Done()
	for range concurrencyChan {
		start := time.Now()
		body := Request{
			Model: "test",
			Messages: []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{{Role: "user", Content: "Hello"}},
			MaxTokens: 50,
		}
		b, _ := json.Marshal(body)
		resp, err := http.Post("http://localhost:9000/v1/chat/completions", "application/json", bytes.NewReader(b))
		if err == nil {
			resp.Body.Close()
		}
		stats <- time.Since(start)
		if err != nil {
			log.Printf("worker %d: request failed: %v", id, err)
		} else {
			log.Printf("worker %d: success, latency=%v", id, time.Since(start))
		}
		
	}
}

func main() {
	concurrency := 10               // start with 10 parallel requests
	duration := 30 * time.Second
	stop := time.After(duration)

	concurrencyChan := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	stats := make(chan time.Duration, 1000)

	// Launch workers
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go worker(i, concurrencyChan, &wg, stats)
	}

	// Feed the channel as fast as workers can consume (closed‑loop)
	go func() {
		for {
			select {
			case <-stop:
				close(concurrencyChan)
				return
			default:
				concurrencyChan <- struct{}{}
			}
		}
	}()

	wg.Wait()
	close(stats)

	// Print summary
	var total time.Duration
	var count int
	for d := range stats {
		total += d
		count++
	}
	if count > 0 {
		fmt.Printf("Sent %d requests, avg latency = %v\n", count, total/time.Duration(count))
	}
}