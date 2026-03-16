package services

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/alexnetofit/api-leona/configs"
)

// ProxyInfo holds the runtime state for a single proxy in the pool.
type ProxyInfo struct {
	IP               string    `json:"ip"`
	Provider         string    `json:"provider"`
	SessionID        string    `json:"session_id"`
	Status           string    `json:"status"`
	AssignedInstance string    `json:"assigned_instance,omitempty"`
	HealthScore      int       `json:"health_score"`
	LastCheck        time.Time `json:"last_check"`
}

// ProxyManager manages the proxy pool for WhatsApp instances.
// BrightData is the primary provider; IPRoyal serves as failover.
type ProxyManager struct {
	db      *DB
	cfg     *configs.Config
	proxies map[string]*ProxyInfo // keyed by IP
	mu      sync.RWMutex
	stopCh  chan struct{}
}

// NewProxyManager creates a ProxyManager and loads existing proxies from the database.
func NewProxyManager(db *DB, cfg *configs.Config) *ProxyManager {
	return &ProxyManager{
		db:      db,
		cfg:     cfg,
		proxies: make(map[string]*ProxyInfo),
		stopCh:  make(chan struct{}),
	}
}

// Start launches the background health-check goroutine.
func (pm *ProxyManager) Start() {
	go pm.healthCheckLoop()
	log.Println("[ProxyManager] started health-check loop")
}

// Stop signals the health-check goroutine to exit.
func (pm *ProxyManager) Stop() {
	close(pm.stopCh)
	log.Println("[ProxyManager] stopped")
}

// AssignProxy selects an available proxy for the given instance.
// It tries the primary provider (brightdata) first, then the failover (iproyal).
func (pm *ProxyManager) AssignProxy(instanceID string) (*ProxyInfo, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Try primary provider first.
	proxy := pm.findAvailable(pm.cfg.Proxy.PrimaryProvider)
	if proxy == nil {
		// Determine failover provider.
		failover := "iproyal"
		if pm.cfg.Proxy.PrimaryProvider == "iproyal" {
			failover = "brightdata"
		}
		proxy = pm.findAvailable(failover)
	}

	if proxy == nil {
		return nil, fmt.Errorf("no available proxy in pool")
	}

	proxy.Status = "assigned"
	proxy.AssignedInstance = instanceID

	// Persist to database.
	dbProxy, err := pm.db.GetAvailableProxy(proxy.Provider)
	if err != nil {
		return nil, fmt.Errorf("failed to query proxy from database: %w", err)
	}
	if dbProxy != nil {
		if err := pm.db.AssignProxy(dbProxy.ID, instanceID); err != nil {
			return nil, fmt.Errorf("failed to persist proxy assignment: %w", err)
		}
	}

	// Update instance record with proxy details.
	if err := pm.db.UpdateInstanceProxy(instanceID, proxy.IP, proxy.Provider, proxy.SessionID); err != nil {
		log.Printf("[ProxyManager] warning: failed to update instance proxy columns: %v", err)
	}

	log.Printf("[ProxyManager] assigned proxy %s (%s) to instance %s", proxy.IP, proxy.Provider, instanceID)
	return proxy, nil
}

// ReleaseProxy marks the proxy assigned to the given instance as available.
func (pm *ProxyManager) ReleaseProxy(instanceID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	found := false
	for _, p := range pm.proxies {
		if p.AssignedInstance == instanceID {
			p.Status = "available"
			p.AssignedInstance = ""
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("no proxy assigned to instance %s", instanceID)
	}

	if err := pm.db.ReleaseProxy(instanceID); err != nil {
		return fmt.Errorf("failed to release proxy in database: %w", err)
	}

	// Clear proxy columns on the instance.
	if err := pm.db.UpdateInstanceProxy(instanceID, "", "", ""); err != nil {
		log.Printf("[ProxyManager] warning: failed to clear instance proxy columns: %v", err)
	}

	log.Printf("[ProxyManager] released proxy for instance %s", instanceID)
	return nil
}

// ReplaceProxy marks the current proxy for an instance as dead and assigns a new one.
func (pm *ProxyManager) ReplaceProxy(instanceID string) (*ProxyInfo, error) {
	pm.mu.Lock()

	// Find and mark the current proxy as dead.
	for _, p := range pm.proxies {
		if p.AssignedInstance == instanceID {
			p.Status = "dead"
			p.HealthScore = 0
			p.AssignedInstance = ""

			if err := pm.db.MarkProxyDead(p.IP); err != nil {
				log.Printf("[ProxyManager] warning: failed to mark proxy %s dead in DB: %v", p.IP, err)
			}
			break
		}
	}

	pm.mu.Unlock()

	// Assign a fresh proxy.
	return pm.AssignProxy(instanceID)
}

// GetProxyURL builds the full proxy URL string for use by the WhatsApp client.
func (pm *ProxyManager) GetProxyURL(info *ProxyInfo) string {
	switch info.Provider {
	case "brightdata":
		bd := pm.cfg.Proxy.BrightData
		userInfo := fmt.Sprintf("brd-customer-%s-zone-%s-ip-%s", bd.CustomerID, bd.Zone, info.IP)
		u := url.URL{
			Scheme: "http",
			User:   url.UserPassword(userInfo, bd.Password),
			Host:   bd.Endpoint,
		}
		return u.String()

	case "iproyal":
		ir := pm.cfg.Proxy.IPRoyal
		u := url.URL{
			Scheme: "http",
			User:   url.UserPassword(ir.Username, ir.Password),
			Host:   ir.Endpoint,
		}
		return u.String()

	default:
		return ""
	}
}

// GetProxyStatus returns a snapshot of all proxy states.
func (pm *ProxyManager) GetProxyStatus() []ProxyInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make([]ProxyInfo, 0, len(pm.proxies))
	for _, p := range pm.proxies {
		result = append(result, *p)
	}
	return result
}

// GetProxyHealth returns the health information for a specific proxy IP.
func (pm *ProxyManager) GetProxyHealth(ip string) (*ProxyInfo, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	p, ok := pm.proxies[ip]
	if !ok {
		return nil, fmt.Errorf("proxy %s not found in pool", ip)
	}

	cp := *p
	return &cp, nil
}

// SeedProxies adds a batch of IPs to the in-memory pool and persists them to the database.
func (pm *ProxyManager) SeedProxies(ips []string, provider string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for _, ip := range ips {
		if _, exists := pm.proxies[ip]; exists {
			continue
		}

		pm.proxies[ip] = &ProxyInfo{
			IP:          ip,
			Provider:    provider,
			Status:      "available",
			HealthScore: 100,
			LastCheck:   time.Now(),
		}

		// Insert into database. Use a simple INSERT with conflict handling.
		_, err := pm.db.Conn.Exec(`
			INSERT INTO proxy_pool (provider, ip_address, status, health_score, last_check_at)
			VALUES ($1, $2, 'available', 100, NOW())
			ON CONFLICT (ip_address) DO NOTHING
		`, provider, ip)
		if err != nil {
			return fmt.Errorf("failed to seed proxy %s: %w", ip, err)
		}
	}

	log.Printf("[ProxyManager] seeded %d proxies for provider %s", len(ips), provider)
	return nil
}

// healthCheckLoop runs periodically to verify connectivity of all assigned proxies.
func (pm *ProxyManager) healthCheckLoop() {
	interval := time.Duration(pm.cfg.Proxy.HealthCheckIntervalSecs) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-pm.stopCh:
			return
		case <-ticker.C:
			pm.runHealthChecks()
		}
	}
}

// runHealthChecks iterates over assigned proxies and updates their health scores.
func (pm *ProxyManager) runHealthChecks() {
	pm.mu.RLock()
	targets := make([]*ProxyInfo, 0)
	for _, p := range pm.proxies {
		if p.Status == "assigned" || p.Status == "degraded" {
			targets = append(targets, p)
		}
	}
	pm.mu.RUnlock()

	for _, p := range targets {
		latencyMs, err := pm.checkProxy(p)

		pm.mu.Lock()
		p.LastCheck = time.Now()

		if err != nil {
			// Connectivity failure: decrease score aggressively.
			p.HealthScore -= 34
			if p.HealthScore < 0 {
				p.HealthScore = 0
			}

			if p.HealthScore == 0 {
				p.Status = "dead"
				log.Printf("[ProxyManager] proxy %s is dead (health=0)", p.IP)

				if dbErr := pm.db.MarkProxyDead(p.IP); dbErr != nil {
					log.Printf("[ProxyManager] warning: failed to mark proxy %s dead in DB: %v", p.IP, dbErr)
				}
			}
		} else {
			// Successful connection.
			p.HealthScore += 10
			if p.HealthScore > 100 {
				p.HealthScore = 100
			}

			if latencyMs > 500 {
				p.Status = "degraded"
			} else if p.Status == "degraded" {
				// Restore to assigned once latency recovers.
				p.Status = "assigned"
			}
		}

		// Persist updated health score.
		if dbErr := pm.db.UpdateProxyHealth(p.IP, p.HealthScore); dbErr != nil {
			log.Printf("[ProxyManager] warning: failed to update health for %s: %v", p.IP, dbErr)
		}

		pm.mu.Unlock()
	}
}

// checkProxy performs a TCP dial to the proxy endpoint and returns latency in milliseconds.
func (pm *ProxyManager) checkProxy(info *ProxyInfo) (latencyMs int, err error) {
	var endpoint string

	switch info.Provider {
	case "brightdata":
		endpoint = pm.cfg.Proxy.BrightData.Endpoint
	case "iproyal":
		endpoint = pm.cfg.Proxy.IPRoyal.Endpoint
	default:
		return 0, fmt.Errorf("unknown provider: %s", info.Provider)
	}

	start := time.Now()
	conn, err := net.DialTimeout("tcp", endpoint, 5*time.Second)
	elapsed := time.Since(start)

	if err != nil {
		return 0, fmt.Errorf("tcp dial to %s failed: %w", endpoint, err)
	}
	conn.Close()

	return int(elapsed.Milliseconds()), nil
}

// GetProxyURLByIP builds a proxy URL given an IP and provider.
func (pm *ProxyManager) GetProxyURLByIP(ip, provider string) string {
	if ip == "" || provider == "" {
		return ""
	}
	info := &ProxyInfo{IP: ip, Provider: provider}
	return pm.GetProxyURL(info)
}

func (pm *ProxyManager) findAvailable(provider string) *ProxyInfo {
	var best *ProxyInfo
	for _, p := range pm.proxies {
		if p.Provider == provider && p.Status == "available" {
			if best == nil || p.HealthScore > best.HealthScore {
				best = p
			}
		}
	}
	return best
}
