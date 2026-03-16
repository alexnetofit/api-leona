package handlers

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v2"

	"github.com/alexnetofit/api-leona/pkg/whatsapp"
)

type MessageHandler struct {
	manager *whatsapp.Manager
}

func NewMessageHandler(manager *whatsapp.Manager) *MessageHandler {
	return &MessageHandler{manager: manager}
}

func (h *MessageHandler) getClient(c *fiber.Ctx) (*whatsapp.Client, error) {
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

func (h *MessageHandler) SendText(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	var req struct {
		Phone string `json:"phone"`
		Text  string `json:"text"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	msgID, err := client.SendText(req.Phone, req.Text)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to send: %v", err)})
	}

	return c.JSON(fiber.Map{"message_id": msgID, "status": "sent"})
}

func (h *MessageHandler) SendImage(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	var req struct {
		Phone    string `json:"phone"`
		Image    string `json:"image"`
		Caption  string `json:"caption"`
		MimeType string `json:"mime_type"`
		URL      string `json:"url"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	data, mimeType, err := resolveMedia(req.Image, req.URL, req.MimeType, "image/jpeg")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	msgID, err := client.SendImage(req.Phone, data, req.Caption, mimeType)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to send: %v", err)})
	}

	return c.JSON(fiber.Map{"message_id": msgID, "status": "sent"})
}

func (h *MessageHandler) SendVideo(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	var req struct {
		Phone    string `json:"phone"`
		Video    string `json:"video"`
		Caption  string `json:"caption"`
		MimeType string `json:"mime_type"`
		URL      string `json:"url"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	data, mimeType, err := resolveMedia(req.Video, req.URL, req.MimeType, "video/mp4")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	msgID, err := client.SendVideo(req.Phone, data, req.Caption, mimeType)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to send: %v", err)})
	}

	return c.JSON(fiber.Map{"message_id": msgID, "status": "sent"})
}

func (h *MessageHandler) SendAudio(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	var req struct {
		Phone    string `json:"phone"`
		Audio    string `json:"audio"`
		MimeType string `json:"mime_type"`
		URL      string `json:"url"`
		PTT      bool   `json:"ptt"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	data, mimeType, err := resolveMedia(req.Audio, req.URL, req.MimeType, "audio/ogg")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	msgID, err := client.SendAudio(req.Phone, data, mimeType, req.PTT)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to send: %v", err)})
	}

	return c.JSON(fiber.Map{"message_id": msgID, "status": "sent"})
}

func (h *MessageHandler) SendDocument(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	var req struct {
		Phone    string `json:"phone"`
		Document string `json:"document"`
		Filename string `json:"filename"`
		MimeType string `json:"mime_type"`
		URL      string `json:"url"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	data, mimeType, err := resolveMedia(req.Document, req.URL, req.MimeType, "application/octet-stream")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	msgID, err := client.SendDocument(req.Phone, data, req.Filename, mimeType)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to send: %v", err)})
	}

	return c.JSON(fiber.Map{"message_id": msgID, "status": "sent"})
}

func (h *MessageHandler) SendSticker(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	var req struct {
		Phone    string `json:"phone"`
		Sticker  string `json:"sticker"`
		MimeType string `json:"mime_type"`
		URL      string `json:"url"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	data, mimeType, err := resolveMedia(req.Sticker, req.URL, req.MimeType, "image/webp")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	msgID, err := client.SendSticker(req.Phone, data, mimeType)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to send: %v", err)})
	}

	return c.JSON(fiber.Map{"message_id": msgID, "status": "sent"})
}

func (h *MessageHandler) SendLocation(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	var req struct {
		Phone   string  `json:"phone"`
		Lat     float64 `json:"latitude"`
		Lng     float64 `json:"longitude"`
		Name    string  `json:"name"`
		Address string  `json:"address"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	msgID, err := client.SendLocation(req.Phone, req.Lat, req.Lng, req.Name, req.Address)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to send: %v", err)})
	}

	return c.JSON(fiber.Map{"message_id": msgID, "status": "sent"})
}

func (h *MessageHandler) SendContact(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	var req struct {
		Phone        string `json:"phone"`
		ContactName  string `json:"contact_name"`
		ContactPhone string `json:"contact_phone"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	msgID, err := client.SendContact(req.Phone, req.ContactName, req.ContactPhone)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to send: %v", err)})
	}

	return c.JSON(fiber.Map{"message_id": msgID, "status": "sent"})
}

func (h *MessageHandler) SendReaction(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	var req struct {
		Phone     string `json:"phone"`
		MessageID string `json:"message_id"`
		Emoji     string `json:"emoji"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	if err := client.SendReaction(req.Phone, req.MessageID, req.Emoji); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to send: %v", err)})
	}

	return c.JSON(fiber.Map{"status": "sent"})
}

func (h *MessageHandler) SendPoll(c *fiber.Ctx) error {
	client, err := h.getClient(c)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}

	var req struct {
		Phone         string   `json:"phone"`
		Question      string   `json:"question"`
		Options       []string `json:"options"`
		MaxSelections int      `json:"max_selections"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
	}

	msgID, err := client.SendPoll(req.Phone, req.Question, req.Options, req.MaxSelections)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": fmt.Sprintf("failed to send: %v", err)})
	}

	return c.JSON(fiber.Map{"message_id": msgID, "status": "sent"})
}

func (h *MessageHandler) SendPresence(c *fiber.Ctx) error {
	// Presence sending is handled differently - for now return success
	return c.JSON(fiber.Map{"status": "sent"})
}

func (h *MessageHandler) MarkRead(c *fiber.Ctx) error {
	// Mark as read placeholder
	return c.JSON(fiber.Map{"status": "sent"})
}

func resolveMedia(b64Data, url, mimeType, defaultMime string) ([]byte, string, error) {
	if mimeType == "" {
		mimeType = defaultMime
	}

	if b64Data != "" {
		data, err := decodeBase64(b64Data)
		if err != nil {
			return nil, "", fmt.Errorf("invalid base64 data: %v", err)
		}
		return data, mimeType, nil
	}

	if url != "" {
		data, err := downloadMedia(url)
		if err != nil {
			return nil, "", fmt.Errorf("failed to download media: %v", err)
		}
		return data, mimeType, nil
	}

	return nil, "", fmt.Errorf("no media provided (use base64 data or url)")
}

func downloadMedia(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func decodeBase64(data string) ([]byte, error) {
	if idx := strings.Index(data, ","); idx != -1 {
		data = data[idx+1:]
	}
	return base64.StdEncoding.DecodeString(data)
}
