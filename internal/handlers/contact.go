package handlers

import (
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/alexnetofit/api-leona/pkg/whatsapp"
)

type ContactHandler struct {
	manager *whatsapp.Manager
}

func NewContactHandler(manager *whatsapp.Manager) *ContactHandler {
	return &ContactHandler{manager: manager}
}

func (h *ContactHandler) getClient(c *fiber.Ctx) (*whatsapp.Client, error) {
	id := c.Params("id")
	client, exists := h.manager.GetInstance(id)
	if !exists {
		return nil, fmt.Errorf("instance not found")
	}
	if !client.IsConnected() {
		return nil, fmt.Errorf("instance not connected")
	}
	return client, nil
}

func (h *ContactHandler) Check(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	var req struct {
		Phone string `json:"phone"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	registered, err := client.CheckRegistered(req.Phone)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to check: %v", err)})
	}

	return c.JSON(fiber.Map{"phone": req.Phone, "registered": registered})
}

func (h *ContactHandler) GetPicture(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	phone := c.Params("phone")
	url, err := client.GetProfilePicture(phone)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to get picture: %v", err)})
	}

	return c.JSON(fiber.Map{"phone": phone, "picture_url": url})
}

func (h *ContactHandler) GetStatus(c *fiber.Ctx) error {
	// Status/about requires subscription to presence - simplified response
	return c.JSON(fiber.Map{"phone": c.Params("phone"), "status": ""})
}
