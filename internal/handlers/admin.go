package handlers

import (
	"github.com/gofiber/fiber/v2"

	"github.com/alexnetofit/api-leona/internal/services"
	"github.com/alexnetofit/api-leona/pkg/whatsapp"
)

type AdminHandler struct {
	manager      *whatsapp.Manager
	proxyManager *services.ProxyManager
}

func NewAdminHandler(manager *whatsapp.Manager, pm *services.ProxyManager) *AdminHandler {
	return &AdminHandler{
		manager:      manager,
		proxyManager: pm,
	}
}

func (h *AdminHandler) Health(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
}

func (h *AdminHandler) ProxiesStatus(c *fiber.Ctx) error {
	proxies := h.proxyManager.GetProxyStatus()
	return c.JSON(fiber.Map{"proxies": proxies})
}

func (h *AdminHandler) ProxyHealth(c *fiber.Ctx) error {
	ip := c.Params("ip")
	info, err := h.proxyManager.GetProxyHealth(ip)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(info)
}

func (h *AdminHandler) ReplaceProxy(c *fiber.Ctx) error {
	ip := c.Params("ip")
	_ = ip
	var req struct {
		InstanceID string `json:"instance_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	newProxy, err := h.proxyManager.ReplaceProxy(req.InstanceID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"status":   "replaced",
		"new_ip":   newProxy.IP,
		"provider": newProxy.Provider,
	})
}

func (h *AdminHandler) Stats(c *fiber.Ctx) error {
	instances := h.manager.GetAllInstances()

	connected := 0
	disconnected := 0
	for _, client := range instances {
		if client.IsConnected() {
			connected++
		} else {
			disconnected++
		}
	}

	return c.JSON(fiber.Map{
		"total_instances":        len(instances),
		"connected_instances":    connected,
		"disconnected_instances": disconnected,
	})
}
