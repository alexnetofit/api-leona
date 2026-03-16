package services

import (
	"log"
	"sync"
	"time"

	"github.com/alexnetofit/api-leona/pkg/whatsapp"
)

type HealthChecker struct {
	manager      *whatsapp.Manager
	db           *DB
	proxyManager *ProxyManager
	stopCh       chan struct{}
	mu           sync.Mutex
}

func NewHealthChecker(manager *whatsapp.Manager, db *DB, proxyManager *ProxyManager) *HealthChecker {
	return &HealthChecker{
		manager:      manager,
		db:           db,
		proxyManager: proxyManager,
		stopCh:       make(chan struct{}),
	}
}

func (hc *HealthChecker) Start(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-hc.stopCh:
				return
			case <-ticker.C:
				hc.check()
			}
		}
	}()
	log.Printf("[HealthChecker] started with interval %v", interval)
}

func (hc *HealthChecker) Stop() {
	close(hc.stopCh)
}

func (hc *HealthChecker) check() {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	instances := hc.manager.GetAllInstances()
	disconnected := 0
	total := len(instances)

	for id, client := range instances {
		if !client.IsConnected() {
			disconnected++
			log.Printf("[HealthChecker] instance %s disconnected", id)
		}
	}

	if total > 0 && float64(disconnected)/float64(total) > 0.1 {
		log.Printf("[HealthChecker] WARNING: %d/%d instances disconnected (>10%%)", disconnected, total)
	}
}
