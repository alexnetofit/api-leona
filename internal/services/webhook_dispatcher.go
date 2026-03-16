package services

import (
	"bytes"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/alexnetofit/api-leona/configs"
)

type webhookJob struct {
	InstanceID string
	Event      string
	WebhookURL string
	Payload    []byte
	Attempts   int
}

type WebhookDispatcher struct {
	cfg     *configs.Config
	db      *DB
	queue   chan webhookJob
	stopCh  chan struct{}
	wg      sync.WaitGroup
	client  *http.Client
}

func NewWebhookDispatcher(cfg *configs.Config, db *DB) *WebhookDispatcher {
	return &WebhookDispatcher{
		cfg:    cfg,
		db:     db,
		queue:  make(chan webhookJob, 10000),
		stopCh: make(chan struct{}),
		client: &http.Client{
			Timeout: time.Duration(cfg.Webhook.TimeoutSeconds) * time.Second,
		},
	}
}

func (wd *WebhookDispatcher) Start(workers int) {
	for i := 0; i < workers; i++ {
		wd.wg.Add(1)
		go wd.worker()
	}
	log.Printf("[WebhookDispatcher] started with %d workers", workers)
}

func (wd *WebhookDispatcher) Stop() {
	close(wd.stopCh)
	wd.wg.Wait()
	log.Println("[WebhookDispatcher] stopped")
}

func (wd *WebhookDispatcher) Dispatch(instanceID, event, instanceWebhookURL string, payload []byte) {
	if instanceWebhookURL != "" {
		select {
		case wd.queue <- webhookJob{
			InstanceID: instanceID,
			Event:      event,
			WebhookURL: instanceWebhookURL,
			Payload:    payload,
		}:
		default:
			log.Printf("[WebhookDispatcher] queue full, dropping instance webhook for %s", instanceID)
		}
	}

	if wd.cfg.Webhook.GlobalURL != "" {
		select {
		case wd.queue <- webhookJob{
			InstanceID: instanceID,
			Event:      event,
			WebhookURL: wd.cfg.Webhook.GlobalURL,
			Payload:    payload,
		}:
		default:
			log.Printf("[WebhookDispatcher] queue full, dropping global webhook for %s", instanceID)
		}
	}
}

func (wd *WebhookDispatcher) worker() {
	defer wd.wg.Done()

	for {
		select {
		case <-wd.stopCh:
			return
		case job := <-wd.queue:
			wd.send(job)
		}
	}
}

func (wd *WebhookDispatcher) send(job webhookJob) {
	delays := []time.Duration{time.Second, 5 * time.Second, 15 * time.Second}
	maxRetries := wd.cfg.Webhook.MaxRetries

	for attempt := 0; attempt <= maxRetries; attempt++ {
		statusCode, err := wd.doRequest(job)

		if err == nil && statusCode >= 200 && statusCode < 300 {
			if wd.db != nil {
				_ = wd.db.LogWebhook(job.InstanceID, job.Event, string(job.Payload), statusCode, attempt+1)
			}
			return
		}

		logCode := 0
		if statusCode > 0 {
			logCode = statusCode
		}

		if attempt < maxRetries {
			delay := delays[attempt]
			log.Printf("[WebhookDispatcher] attempt %d failed for %s (status=%d), retrying in %v",
				attempt+1, job.WebhookURL, logCode, delay)

			select {
			case <-time.After(delay):
			case <-wd.stopCh:
				return
			}
		} else {
			log.Printf("[WebhookDispatcher] all %d attempts failed for %s/%s",
				maxRetries+1, job.InstanceID, job.Event)
			if wd.db != nil {
				_ = wd.db.LogWebhook(job.InstanceID, job.Event, string(job.Payload), logCode, attempt+1)
			}
		}
	}
}

func (wd *WebhookDispatcher) doRequest(job webhookJob) (int, error) {
	req, err := http.NewRequest("POST", job.WebhookURL, bytes.NewReader(job.Payload))
	if err != nil {
		return 0, err
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range wd.cfg.Webhook.Headers {
		req.Header.Set(k, v)
	}

	resp, err := wd.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	return resp.StatusCode, nil
}
