package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"

	"github.com/alexnetofit/api-leona/internal/services"
	"github.com/alexnetofit/api-leona/pkg/whatsapp"
)

type InstanceHandler struct {
	manager      *whatsapp.Manager
	db           *services.DB
	proxyManager *services.ProxyManager
}

func NewInstanceHandler(manager *whatsapp.Manager, db *services.DB, pm *services.ProxyManager) *InstanceHandler {
	return &InstanceHandler{
		manager:      manager,
		db:           db,
		proxyManager: pm,
	}
}

func (h *InstanceHandler) Create(c *fiber.Ctx) error {
	var req struct {
		Name       string `json:"name"`
		WebhookURL string `json:"webhook_url"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	id := generateID()

	proxyAddr := ""
	proxyIP := ""
	proxyProvider := ""
	proxySessionID := ""

	proxy, err := h.proxyManager.AssignProxy(id)
	if err != nil {
		log.Printf("[InstanceHandler] no proxy available: %v (creating without proxy)", err)
	} else {
		proxyAddr = h.proxyManager.GetProxyURL(proxy)
		proxyIP = proxy.IP
		proxyProvider = proxy.Provider
		proxySessionID = proxy.SessionID
	}

	client, err := h.manager.CreateInstance(id, req.Name, req.WebhookURL, proxyAddr)
	if err != nil {
		if proxy != nil {
			_ = h.proxyManager.ReleaseProxy(id)
		}
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to create instance: %v", err)})
	}

	if err := h.db.CreateInstance(id, req.Name, req.WebhookURL, proxyIP, proxyProvider, proxySessionID); err != nil {
		log.Printf("[InstanceHandler] db create error: %v", err)
	}

	return c.Status(201).JSON(fiber.Map{
		"instance_id": id,
		"name":        req.Name,
		"proxy_ip":    proxyIP,
		"status":      "created",
		"connected":   client.IsConnected(),
	})
}

func (h *InstanceHandler) List(c *fiber.Ctx) error {
	instances, err := h.db.ListInstances()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to list instances: %v", err)})
	}
	return c.JSON(fiber.Map{"instances": instances})
}

func (h *InstanceHandler) Info(c *fiber.Ctx) error {
	id := c.Params("id")
	instance, err := h.db.GetInstance(id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": "instance not found"})
	}

	client, exists := h.manager.GetInstance(id)
	connected := false
	if exists {
		connected = client.IsConnected()
	}

	return c.JSON(fiber.Map{
		"instance": instance,
		"connected": connected,
	})
}

func (h *InstanceHandler) Connect(c *fiber.Ctx) error {
	id := c.Params("id")
	client, exists := h.manager.GetInstance(id)
	if !exists {
		return c.Status(404).JSON(fiber.Map{"error": "instance not found"})
	}

	if err := client.Connect(); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to connect: %v", err)})
	}

	_ = h.db.UpdateInstanceStatus(id, "connecting")

	qr := client.GetQR()
	resp := fiber.Map{"status": "connecting"}
	if qr != "" {
		resp["qr_code"] = qr
	}

	return c.JSON(resp)
}

func (h *InstanceHandler) Disconnect(c *fiber.Ctx) error {
	id := c.Params("id")
	client, exists := h.manager.GetInstance(id)
	if !exists {
		return c.Status(404).JSON(fiber.Map{"error": "instance not found"})
	}

	client.Disconnect()
	_ = h.db.UpdateInstanceStatus(id, "disconnected")

	return c.JSON(fiber.Map{"status": "disconnected"})
}

func (h *InstanceHandler) Logout(c *fiber.Ctx) error {
	id := c.Params("id")
	client, exists := h.manager.GetInstance(id)
	if !exists {
		return c.Status(404).JSON(fiber.Map{"error": "instance not found"})
	}

	if err := client.Logout(); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to logout: %v", err)})
	}

	_ = h.db.UpdateInstanceStatus(id, "logged_out")
	return c.JSON(fiber.Map{"status": "logged_out"})
}

func (h *InstanceHandler) Delete(c *fiber.Ctx) error {
	id := c.Params("id")

	if err := h.manager.DeleteInstance(id); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to delete: %v", err)})
	}

	_ = h.proxyManager.ReleaseProxy(id)
	_ = h.db.ReleaseProxy(id)
	_ = h.db.DeleteInstance(id)

	return c.JSON(fiber.Map{"status": "deleted"})
}

func (h *InstanceHandler) SetWebhook(c *fiber.Ctx) error {
	id := c.Params("id")
	var req struct {
		WebhookURL string `json:"webhook_url"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	client, exists := h.manager.GetInstance(id)
	if !exists {
		return c.Status(404).JSON(fiber.Map{"error": "instance not found"})
	}
	client.WebhookURL = req.WebhookURL

	if err := h.db.UpdateInstanceWebhook(id, req.WebhookURL); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to update webhook"})
	}

	return c.JSON(fiber.Map{"status": "updated", "webhook_url": req.WebhookURL})
}

func (h *InstanceHandler) GetQRCode(c *fiber.Ctx) error {
	id := c.Params("id")
	client, exists := h.manager.GetInstance(id)
	if !exists {
		return c.Status(404).JSON(fiber.Map{"error": "instance not found"})
	}

	qr := client.GetQR()
	if qr == "" {
		return c.Status(404).JSON(fiber.Map{"error": "no QR code available, call /connect first"})
	}

	return c.JSON(fiber.Map{"qr_code": qr})
}

func (h *InstanceHandler) GetQRCodeImage(c *fiber.Ctx) error {
	id := c.Params("id")
	client, exists := h.manager.GetInstance(id)
	if !exists {
		return c.Status(404).JSON(fiber.Map{"error": "instance not found"})
	}

	qr := client.GetQR()
	if qr == "" {
		return c.Status(404).JSON(fiber.Map{"error": "no QR code available, call /connect first"})
	}

	// Return QR as text - client should use a QR library to render
	return c.JSON(fiber.Map{
		"qr_code": qr,
		"hint":    "Use any QR code library to render this string as an image",
	})
}

func (h *InstanceHandler) Restart(c *fiber.Ctx) error {
	id := c.Params("id")
	client, exists := h.manager.GetInstance(id)
	if !exists {
		return c.Status(404).JSON(fiber.Map{"error": "instance not found"})
	}

	client.Disconnect()
	if err := client.Connect(); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to restart: %v", err)})
	}

	return c.JSON(fiber.Map{"status": "restarted"})
}

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
