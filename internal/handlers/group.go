package handlers

import (
	"fmt"

	"github.com/gofiber/fiber/v2"

	"github.com/alexnetofit/api-leona/pkg/whatsapp"
)

type GroupHandler struct {
	manager *whatsapp.Manager
}

func NewGroupHandler(manager *whatsapp.Manager) *GroupHandler {
	return &GroupHandler{manager: manager}
}

func (h *GroupHandler) getClient(c *fiber.Ctx) (*whatsapp.Client, error) {
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

func (h *GroupHandler) Create(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	var req struct {
		Name         string   `json:"name"`
		Participants []string `json:"participants"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	groupJID, err := client.CreateGroup(req.Name, req.Participants)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to create group: %v", err)})
	}

	return c.Status(201).JSON(fiber.Map{"group_jid": groupJID, "status": "created"})
}

func (h *GroupHandler) List(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	groups, err := client.GetJoinedGroups()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to list groups: %v", err)})
	}

	result := make([]fiber.Map, 0, len(groups))
	for _, g := range groups {
		result = append(result, fiber.Map{
			"jid":    g.JID.String(),
			"name":   g.Name,
			"topic":  g.Topic,
			"locked": g.IsLocked,
		})
	}

	return c.JSON(fiber.Map{"groups": result})
}

func (h *GroupHandler) Info(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	groupJID := c.Params("groupJID")
	info, err := client.GetGroupInfo(groupJID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to get group info: %v", err)})
	}

	participants := make([]fiber.Map, 0, len(info.Participants))
	for _, p := range info.Participants {
		participants = append(participants, fiber.Map{
			"jid":      p.JID.String(),
			"is_admin": p.IsAdmin,
			"is_super": p.IsSuperAdmin,
		})
	}

	return c.JSON(fiber.Map{
		"jid":          info.JID.String(),
		"name":         info.Name,
		"topic":        info.Topic,
		"locked":       info.IsLocked,
		"participants": participants,
	})
}

func (h *GroupHandler) AddParticipants(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	groupJID := c.Params("groupJID")
	var req struct {
		Participants []string `json:"participants"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	if err := client.AddGroupParticipants(groupJID, req.Participants); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to add participants: %v", err)})
	}

	return c.JSON(fiber.Map{"status": "added"})
}

func (h *GroupHandler) RemoveParticipants(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	groupJID := c.Params("groupJID")
	var req struct {
		Participants []string `json:"participants"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	if err := client.RemoveGroupParticipants(groupJID, req.Participants); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to remove participants: %v", err)})
	}

	return c.JSON(fiber.Map{"status": "removed"})
}

func (h *GroupHandler) Leave(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	groupJID := c.Params("groupJID")
	if err := client.LeaveGroup(groupJID); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to leave group: %v", err)})
	}

	return c.JSON(fiber.Map{"status": "left"})
}

func (h *GroupHandler) GetInviteLink(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	groupJID := c.Params("groupJID")
	link, err := client.GetGroupInviteLink(groupJID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to get invite link: %v", err)})
	}

	return c.JSON(fiber.Map{"invite_link": link})
}
