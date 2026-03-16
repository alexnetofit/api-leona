package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexnetofit/api-leona/configs"
	"github.com/alexnetofit/api-leona/internal/gateway"
	"github.com/alexnetofit/api-leona/internal/services"
	"github.com/alexnetofit/api-leona/pkg/whatsapp"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("[LeonaAPI] starting...")

	cfg, err := configs.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Ensure storage directory exists
	if err := os.MkdirAll(cfg.Database.SQLitePath, 0755); err != nil {
		log.Fatalf("failed to create storage dir: %v", err)
	}

	// Database
	db, err := services.NewDB(cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.RunMigrations(); err != nil {
		log.Printf("[WARN] migrations error (may be ok if already applied): %v", err)
	}

	// Webhook dispatcher
	webhookDispatcher := services.NewWebhookDispatcher(cfg, db)
	webhookDispatcher.Start(10)
	defer webhookDispatcher.Stop()

	// Event bridge
	eventBridge := services.NewEventBridge(webhookDispatcher, db)

	// WhatsApp manager
	manager := whatsapp.NewManager(cfg.Database.SQLitePath, eventBridge.HandleEvent)

	// Proxy manager
	proxyManager := services.NewProxyManager(db, cfg)
	proxyManager.Start()
	defer proxyManager.Stop()

	// Health checker
	healthChecker := services.NewHealthChecker(manager, db, proxyManager)
	healthChecker.Start(60 * time.Second)
	defer healthChecker.Stop()

	// Restore existing sessions
	restored := manager.RestoreAll()
	if len(restored) > 0 {
		log.Printf("[LeonaAPI] restored %d instances", len(restored))

		// Reconnect restored instances
		for _, id := range restored {
			client, exists := manager.GetInstance(id)
			if !exists {
				continue
			}
			// Get proxy from DB
			instance, err := db.GetInstance(id)
			if err == nil && instance != nil && instance.ProxyIP != "" {
				proxyURL := proxyManager.GetProxyURLByIP(instance.ProxyIP, instance.ProxyProvider)
				if proxyURL != "" {
					client.SetProxy(proxyURL)
				}
			}
			go func(c *whatsapp.Client, instanceID string) {
				if err := c.Connect(); err != nil {
					log.Printf("[LeonaAPI] failed to reconnect %s: %v", instanceID, err)
				} else {
					log.Printf("[LeonaAPI] reconnected %s", instanceID)
				}
			}(client, id)
		}
	}

	// Setup HTTP router
	app := gateway.SetupRouter(cfg, manager, db, proxyManager)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
		log.Printf("[LeonaAPI] listening on %s", addr)
		if err := app.Listen(addr); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-quit
	log.Println("[LeonaAPI] shutting down...")
	_ = app.Shutdown()
	log.Println("[LeonaAPI] goodbye")
}
