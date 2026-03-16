package configs

import (
	"encoding/json"
	"os"
	"strconv"
)

type Config struct {
	Server       ServerConfig       `json:"server"`
	Database     DatabaseConfig     `json:"database"`
	Redis        RedisConfig        `json:"redis"`
	Proxy        ProxyConfig        `json:"proxy"`
	Webhook      WebhookConfig      `json:"webhook"`
	Auth         AuthConfig         `json:"auth"`
	Reconnection ReconnectionConfig `json:"reconnection"`
	Mode         string             `json:"mode"`
	MaxInstances int                `json:"max_instances"`
}

type ServerConfig struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	BodyLimitMB int    `json:"body_limit_mb"`
}

type DatabaseConfig struct {
	SQLitePath  string `json:"sqlite_path"`
	PostgresURL string `json:"postgres_url"`
}

type RedisConfig struct {
	URL string `json:"url"`
}

type ProxyConfig struct {
	PrimaryProvider          string          `json:"primary_provider"`
	BrightData               BrightDataConfig `json:"brightdata"`
	IPRoyal                  IPRoyalConfig   `json:"iproyal"`
	HealthCheckIntervalSecs  int             `json:"health_check_interval_seconds"`
	MediaThroughProxy        bool            `json:"media_through_proxy"`
}

type BrightDataConfig struct {
	CustomerID string `json:"customer_id"`
	Zone       string `json:"zone"`
	Password   string `json:"password"`
	Endpoint   string `json:"endpoint"`
}

type IPRoyalConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Endpoint string `json:"endpoint"`
}

type WebhookConfig struct {
	GlobalURL      string            `json:"global_url"`
	Headers        map[string]string `json:"headers"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	MaxRetries     int               `json:"max_retries"`
}

type AuthConfig struct {
	APIKey string `json:"api_key"`
}

type ReconnectionConfig struct {
	MaxRetries      int `json:"max_retries"`
	BaseDelaySeconds int `json:"base_delay_seconds"`
	MaxDelaySeconds  int `json:"max_delay_seconds"`
}

func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Host:        "0.0.0.0",
			Port:        8080,
			BodyLimitMB: 50,
		},
		Database: DatabaseConfig{
			SQLitePath:  "./storage",
			PostgresURL: "",
		},
		Redis: RedisConfig{
			URL: "redis://localhost:6379",
		},
		Proxy: ProxyConfig{
			PrimaryProvider:         "brightdata",
			HealthCheckIntervalSecs: 60,
			MediaThroughProxy:       false,
			BrightData: BrightDataConfig{
				Endpoint: "brd.superproxy.io:22225",
				Zone:     "isp",
			},
			IPRoyal: IPRoyalConfig{
				Endpoint: "geo.iproyal.com:12321",
			},
		},
		Webhook: WebhookConfig{
			Headers:        make(map[string]string),
			TimeoutSeconds: 10,
			MaxRetries:     3,
		},
		Auth: AuthConfig{
			APIKey: "change-me-in-production",
		},
		Reconnection: ReconnectionConfig{
			MaxRetries:       3,
			BaseDelaySeconds: 5,
			MaxDelaySeconds:  60,
		},
		Mode:         "single",
		MaxInstances: 50,
	}

	// Try loading from config.json
	if data, err := os.ReadFile("config.json"); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	}

	// Override with environment variables
	overrideFromEnv(cfg)

	return cfg, nil
}

func overrideFromEnv(cfg *Config) {
	if v := os.Getenv("API_KEY"); v != "" {
		cfg.Auth.APIKey = v
	}
	if v := os.Getenv("POSTGRES_URL"); v != "" {
		cfg.Database.PostgresURL = v
	}
	if v := os.Getenv("REDIS_URL"); v != "" {
		cfg.Redis.URL = v
	}
	if v := os.Getenv("SQLITE_PATH"); v != "" {
		cfg.Database.SQLitePath = v
	}
	if v := os.Getenv("WEBHOOK_URL"); v != "" {
		cfg.Webhook.GlobalURL = v
	}
	if v := os.Getenv("MODE"); v != "" {
		cfg.Mode = v
	}
	if v := os.Getenv("MAX_INSTANCES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxInstances = n
		}
	}
	if v := os.Getenv("PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = n
		}
	}
	if v := os.Getenv("BRIGHTDATA_CUSTOMER_ID"); v != "" {
		cfg.Proxy.BrightData.CustomerID = v
	}
	if v := os.Getenv("BRIGHTDATA_ZONE"); v != "" {
		cfg.Proxy.BrightData.Zone = v
	}
	if v := os.Getenv("BRIGHTDATA_PASSWORD"); v != "" {
		cfg.Proxy.BrightData.Password = v
	}
	if v := os.Getenv("IPROYAL_USERNAME"); v != "" {
		cfg.Proxy.IPRoyal.Username = v
	}
	if v := os.Getenv("IPROYAL_PASSWORD"); v != "" {
		cfg.Proxy.IPRoyal.Password = v
	}
}
