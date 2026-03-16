package services

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/lib/pq"

	"github.com/alexnetofit/api-leona/configs"
)

type Instance struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Phone          string    `json:"phone,omitempty"`
	Status         string    `json:"status"`
	WebhookURL     string    `json:"webhook_url,omitempty"`
	ProxyIP        string    `json:"proxy_ip,omitempty"`
	ProxyProvider  string    `json:"proxy_provider,omitempty"`
	ProxySessionID string    `json:"proxy_session_id,omitempty"`
	WorkerID       string    `json:"worker_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ProxyPool struct {
	ID                 int       `json:"id"`
	Provider           string    `json:"provider"`
	IPAddress          string    `json:"ip_address"`
	SessionID          string    `json:"session_id,omitempty"`
	Status             string    `json:"status"`
	AssignedInstanceID string    `json:"assigned_instance_id,omitempty"`
	HealthScore        int       `json:"health_score"`
	LastCheckAt        time.Time `json:"last_check_at"`
}

type DB struct {
	Conn   *sql.DB
	config *configs.Config
}

func NewDB(cfg *configs.Config) (*DB, error) {
	if cfg.Database.PostgresURL == "" {
		return nil, fmt.Errorf("postgres_url is required")
	}

	conn, err := sql.Open("postgres", cfg.Database.PostgresURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(5 * time.Minute)

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{Conn: conn, config: cfg}, nil
}

func (db *DB) RunMigrations() error {
	migrationPath := filepath.Join("migrations", "001_init.sql")
	data, err := os.ReadFile(migrationPath)
	if err != nil {
		return fmt.Errorf("failed to read migration file: %w", err)
	}

	_, err = db.Conn.Exec(string(data))
	if err != nil {
		return fmt.Errorf("failed to execute migration: %w", err)
	}

	return nil
}

func (db *DB) Close() {
	if db.Conn != nil {
		db.Conn.Close()
	}
}

func (db *DB) CreateInstance(id, name, webhookURL, proxyIP, proxyProvider, proxySessionID string) error {
	stmt, err := db.Conn.Prepare(`
		INSERT INTO instances (id, name, status, webhook_url, proxy_ip, proxy_provider, proxy_session_id, created_at, updated_at)
		VALUES ($1, $2, 'disconnected', $3, $4, $5, $6, NOW(), NOW())
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(id, name, toNullString(webhookURL), toNullString(proxyIP), toNullString(proxyProvider), toNullString(proxySessionID))
	if err != nil {
		return fmt.Errorf("failed to create instance: %w", err)
	}

	return nil
}

func (db *DB) GetInstance(id string) (*Instance, error) {
	stmt, err := db.Conn.Prepare(`
		SELECT id, name, phone, status, webhook_url, proxy_ip, proxy_provider, proxy_session_id, worker_id, created_at, updated_at
		FROM instances WHERE id = $1
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	var inst Instance
	var phone, webhookURL, proxyIP, proxyProvider, proxySessionID, workerID sql.NullString

	err = stmt.QueryRow(id).Scan(
		&inst.ID, &inst.Name, &phone, &inst.Status,
		&webhookURL, &proxyIP, &proxyProvider, &proxySessionID,
		&workerID, &inst.CreatedAt, &inst.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}

	inst.Phone = phone.String
	inst.WebhookURL = webhookURL.String
	inst.ProxyIP = proxyIP.String
	inst.ProxyProvider = proxyProvider.String
	inst.ProxySessionID = proxySessionID.String
	inst.WorkerID = workerID.String

	return &inst, nil
}

func (db *DB) ListInstances() ([]Instance, error) {
	rows, err := db.Conn.Query(`
		SELECT id, name, phone, status, webhook_url, proxy_ip, proxy_provider, proxy_session_id, worker_id, created_at, updated_at
		FROM instances ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list instances: %w", err)
	}
	defer rows.Close()

	var instances []Instance
	for rows.Next() {
		var inst Instance
		var phone, webhookURL, proxyIP, proxyProvider, proxySessionID, workerID sql.NullString

		err := rows.Scan(
			&inst.ID, &inst.Name, &phone, &inst.Status,
			&webhookURL, &proxyIP, &proxyProvider, &proxySessionID,
			&workerID, &inst.CreatedAt, &inst.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan instance: %w", err)
		}

		inst.Phone = phone.String
		inst.WebhookURL = webhookURL.String
		inst.ProxyIP = proxyIP.String
		inst.ProxyProvider = proxyProvider.String
		inst.ProxySessionID = proxySessionID.String
		inst.WorkerID = workerID.String

		instances = append(instances, inst)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating instances: %w", err)
	}

	return instances, nil
}

func (db *DB) UpdateInstanceStatus(id, status string) error {
	stmt, err := db.Conn.Prepare(`UPDATE instances SET status = $1, updated_at = NOW() WHERE id = $2`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	result, err := stmt.Exec(status, id)
	if err != nil {
		return fmt.Errorf("failed to update instance status: %w", err)
	}

	return checkRowsAffected(result, "instance")
}

func (db *DB) UpdateInstanceWebhook(id, webhookURL string) error {
	stmt, err := db.Conn.Prepare(`UPDATE instances SET webhook_url = $1, updated_at = NOW() WHERE id = $2`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	result, err := stmt.Exec(toNullString(webhookURL), id)
	if err != nil {
		return fmt.Errorf("failed to update instance webhook: %w", err)
	}

	return checkRowsAffected(result, "instance")
}

func (db *DB) UpdateInstanceProxy(id, proxyIP, proxyProvider, proxySessionID string) error {
	stmt, err := db.Conn.Prepare(`
		UPDATE instances SET proxy_ip = $1, proxy_provider = $2, proxy_session_id = $3, updated_at = NOW() WHERE id = $4
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	result, err := stmt.Exec(toNullString(proxyIP), toNullString(proxyProvider), toNullString(proxySessionID), id)
	if err != nil {
		return fmt.Errorf("failed to update instance proxy: %w", err)
	}

	return checkRowsAffected(result, "instance")
}

func (db *DB) UpdateInstancePhone(id, phone string) error {
	stmt, err := db.Conn.Prepare(`UPDATE instances SET phone = $1, updated_at = NOW() WHERE id = $2`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	result, err := stmt.Exec(toNullString(phone), id)
	if err != nil {
		return fmt.Errorf("failed to update instance phone: %w", err)
	}

	return checkRowsAffected(result, "instance")
}

func (db *DB) DeleteInstance(id string) error {
	stmt, err := db.Conn.Prepare(`DELETE FROM instances WHERE id = $1`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	result, err := stmt.Exec(id)
	if err != nil {
		return fmt.Errorf("failed to delete instance: %w", err)
	}

	return checkRowsAffected(result, "instance")
}

func (db *DB) GetAvailableProxy(provider string) (*ProxyPool, error) {
	stmt, err := db.Conn.Prepare(`
		SELECT id, provider, ip_address, session_id, status, assigned_instance_id, health_score, last_check_at
		FROM proxy_pool
		WHERE provider = $1 AND status = 'available'
		ORDER BY health_score DESC, last_check_at ASC
		LIMIT 1
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	var proxy ProxyPool
	var sessionID, assignedInstanceID sql.NullString

	err = stmt.QueryRow(provider).Scan(
		&proxy.ID, &proxy.Provider, &proxy.IPAddress, &sessionID,
		&proxy.Status, &assignedInstanceID, &proxy.HealthScore, &proxy.LastCheckAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get available proxy: %w", err)
	}

	proxy.SessionID = sessionID.String
	proxy.AssignedInstanceID = assignedInstanceID.String

	return &proxy, nil
}

func (db *DB) AssignProxy(proxyID int, instanceID string) error {
	stmt, err := db.Conn.Prepare(`
		UPDATE proxy_pool SET status = 'assigned', assigned_instance_id = $1, last_check_at = NOW() WHERE id = $2
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	result, err := stmt.Exec(instanceID, proxyID)
	if err != nil {
		return fmt.Errorf("failed to assign proxy: %w", err)
	}

	return checkRowsAffected(result, "proxy")
}

func (db *DB) ReleaseProxy(instanceID string) error {
	stmt, err := db.Conn.Prepare(`
		UPDATE proxy_pool SET status = 'available', assigned_instance_id = NULL, last_check_at = NOW()
		WHERE assigned_instance_id = $1
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(instanceID)
	if err != nil {
		return fmt.Errorf("failed to release proxy: %w", err)
	}

	return nil
}

func (db *DB) MarkProxyDead(ip string) error {
	stmt, err := db.Conn.Prepare(`
		UPDATE proxy_pool SET status = 'dead', health_score = 0, last_check_at = NOW() WHERE ip_address = $1
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(ip)
	if err != nil {
		return fmt.Errorf("failed to mark proxy dead: %w", err)
	}

	return nil
}

func (db *DB) UpdateProxyHealth(ip string, score int) error {
	stmt, err := db.Conn.Prepare(`UPDATE proxy_pool SET health_score = $1, last_check_at = NOW() WHERE ip_address = $2`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(score, ip)
	if err != nil {
		return fmt.Errorf("failed to update proxy health: %w", err)
	}

	return nil
}

func (db *DB) LogWebhook(instanceID, event, payload string, statusCode, attempts int) error {
	stmt, err := db.Conn.Prepare(`
		INSERT INTO webhook_logs (instance_id, event, payload, status_code, attempts, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(instanceID, event, payload, statusCode, attempts)
	if err != nil {
		return fmt.Errorf("failed to log webhook: %w", err)
	}

	return nil
}

func (db *DB) ValidateAPIKey(key string) bool {
	return key != "" && key == db.config.Auth.APIKey
}

func toNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

func checkRowsAffected(result sql.Result, entity string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("%s not found", entity)
	}
	return nil
}
