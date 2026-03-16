package whatsapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type EventHandler func(instanceID string, event interface{})

type Client struct {
	ID           string
	Name         string
	WebhookURL   string
	ProxyAddr    string
	client       *whatsmeow.Client
	container    *sqlstore.Container
	qrChan       <-chan whatsmeow.QRChannelItem
	currentQR    string
	connected    bool
	eventHandler EventHandler
	mu           sync.RWMutex
	storagePath  string
}

type Manager struct {
	clients      map[string]*Client
	storagePath  string
	eventHandler EventHandler
	mu           sync.RWMutex
}

func NewManager(storagePath string, handler EventHandler) *Manager {
	return &Manager{
		clients:      make(map[string]*Client),
		storagePath:  storagePath,
		eventHandler: handler,
	}
}

func (m *Manager) CreateInstance(id, name, webhookURL, proxyAddr string) (*Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[id]; exists {
		return nil, fmt.Errorf("instance %s already exists", id)
	}

	if err := os.MkdirAll(m.storagePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	dbPath := filepath.Join(m.storagePath, id+".db")
	container, err := sqlstore.New("sqlite3", "file:"+dbPath+"?_foreign_keys=on", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		return nil, fmt.Errorf("failed to get device store: %w", err)
	}

	cli := whatsmeow.NewClient(deviceStore, nil)

	c := &Client{
		ID:           id,
		Name:         name,
		WebhookURL:   webhookURL,
		ProxyAddr:    proxyAddr,
		client:       cli,
		container:    container,
		eventHandler: m.eventHandler,
		storagePath:  m.storagePath,
	}

	if proxyAddr != "" {
		c.SetProxy(proxyAddr)
	}

	c.registerEventHandler()

	m.clients[id] = c
	return c, nil
}

func (m *Manager) GetInstance(id string) (*Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.clients[id]
	return c, ok
}

func (m *Manager) GetAllInstances() map[string]*Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]*Client, len(m.clients))
	for k, v := range m.clients {
		result[k] = v
	}
	return result
}

func (m *Manager) DeleteInstance(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, exists := m.clients[id]
	if !exists {
		return fmt.Errorf("instance %s not found", id)
	}

	c.Disconnect()
	delete(m.clients, id)

	dbPath := filepath.Join(m.storagePath, id+".db")
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove database: %w", err)
	}

	return nil
}

func (m *Manager) RestoreAll() []string {
	entries, err := os.ReadDir(m.storagePath)
	if err != nil {
		return nil
	}

	var restored []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".db")
		if _, exists := m.clients[id]; exists {
			continue
		}

		_, err := m.CreateInstance(id, id, "", "")
		if err != nil {
			continue
		}
		restored = append(restored, id)
	}
	return restored
}

func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client.Store.ID == nil {
		qrChan, err := c.client.GetQRChannel(context.Background())
		if err != nil {
			return fmt.Errorf("failed to get QR channel: %w", err)
		}
		c.qrChan = qrChan

		if err := c.client.Connect(); err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}

		go c.handleQRChannel()
	} else {
		if err := c.client.Connect(); err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
	}

	return nil
}

func (c *Client) handleQRChannel() {
	for item := range c.qrChan {
		switch item.Event {
		case "code":
			c.mu.Lock()
			c.currentQR = item.Code
			c.mu.Unlock()
			if c.eventHandler != nil {
				c.eventHandler(c.ID, map[string]interface{}{
					"type": "connection.qrcode",
					"qr":   item.Code,
				})
			}
		case "success":
			c.mu.Lock()
			c.connected = true
			c.currentQR = ""
			c.mu.Unlock()
		case "timeout":
			c.mu.Lock()
			c.currentQR = ""
			c.mu.Unlock()
		}
	}
}

func (c *Client) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.client.Disconnect()
	c.connected = false
}

func (c *Client) Logout() error {
	err := c.client.Logout()
	c.Disconnect()
	return err
}

func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

func (c *Client) GetQR() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentQR
}

func (c *Client) SetProxy(addr string) {
	if addr == "" {
		return
	}

	c.ProxyAddr = addr

	parsed, err := url.Parse(addr)
	if err != nil {
		return
	}

	if parsed.Scheme == "socks5" || parsed.Scheme == "http" || parsed.Scheme == "https" {
		c.client.SetProxy(func(req *http.Request) (*url.URL, error) {
			if req.URL.Host == "web.whatsapp.com" || strings.HasPrefix(req.URL.Path, "/ws/chat") {
				return parsed, nil
			}
			return nil, nil
		})
	}
}

func (c *Client) ChangeProxy(newAddr string) error {
	c.Disconnect()
	c.SetProxy(newAddr)
	return c.Connect()
}

func (c *Client) registerEventHandler() {
	c.client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Message:
			if c.eventHandler != nil {
				c.eventHandler(c.ID, map[string]interface{}{
					"type":  "message.received",
					"event": v,
				})
			}
		case *events.Receipt:
			if c.eventHandler != nil {
				eventType := "message.receipt"
				switch v.Type {
				case events.ReceiptTypeDelivered:
					eventType = "message.delivered"
				case events.ReceiptTypeRead:
					eventType = "message.read"
				case events.ReceiptTypePlayed:
					eventType = "message.played"
				}
				c.eventHandler(c.ID, map[string]interface{}{
					"type":  eventType,
					"event": v,
				})
			}
		case *events.Connected:
			c.mu.Lock()
			c.connected = true
			c.mu.Unlock()
			if c.eventHandler != nil {
				c.eventHandler(c.ID, map[string]interface{}{
					"type":  "connection.connected",
					"event": v,
				})
			}
		case *events.Disconnected:
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()
			if c.eventHandler != nil {
				c.eventHandler(c.ID, map[string]interface{}{
					"type":  "connection.disconnected",
					"event": v,
				})
			}
		case *events.LoggedOut:
			c.mu.Lock()
			c.connected = false
			c.mu.Unlock()
			if c.eventHandler != nil {
				c.eventHandler(c.ID, map[string]interface{}{
					"type":  "connection.logged_out",
					"event": v,
				})
			}
		case *events.QR:
			if c.eventHandler != nil {
				c.eventHandler(c.ID, map[string]interface{}{
					"type":  "connection.qrcode",
					"event": v,
				})
			}
		case *events.GroupInfo:
			if c.eventHandler != nil {
				c.eventHandler(c.ID, map[string]interface{}{
					"type":  "group.updated",
					"event": v,
				})
			}
		case *events.CallOffer:
			if c.eventHandler != nil {
				c.eventHandler(c.ID, map[string]interface{}{
					"type":  "call.offer",
					"event": v,
				})
			}
		case *events.Presence:
			if c.eventHandler != nil {
				c.eventHandler(c.ID, map[string]interface{}{
					"type":  "presence.update",
					"event": v,
				})
			}
		}
	})
}

func (c *Client) SendText(phone, text string) (string, error) {
	jid := parseJID(phone)
	resp, err := c.client.SendMessage(context.Background(), jid, &waE2E.Message{
		Conversation: proto.String(text),
	})
	if err != nil {
		return "", fmt.Errorf("failed to send text: %w", err)
	}
	return resp.ID, nil
}

func (c *Client) SendImage(phone string, data []byte, caption, mimeType string) (string, error) {
	jid := parseJID(phone)

	uploaded, err := c.client.Upload(context.Background(), data, whatsmeow.MediaImage)
	if err != nil {
		return "", fmt.Errorf("failed to upload image: %w", err)
	}

	msg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			Caption:       proto.String(caption),
			Mimetype:      proto.String(mimeType),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(data))),
		},
	}

	resp, err := c.client.SendMessage(context.Background(), jid, msg)
	if err != nil {
		return "", fmt.Errorf("failed to send image: %w", err)
	}
	return resp.ID, nil
}

func (c *Client) SendVideo(phone string, data []byte, caption, mimeType string) (string, error) {
	jid := parseJID(phone)

	uploaded, err := c.client.Upload(context.Background(), data, whatsmeow.MediaVideo)
	if err != nil {
		return "", fmt.Errorf("failed to upload video: %w", err)
	}

	msg := &waE2E.Message{
		VideoMessage: &waE2E.VideoMessage{
			Caption:       proto.String(caption),
			Mimetype:      proto.String(mimeType),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(data))),
		},
	}

	resp, err := c.client.SendMessage(context.Background(), jid, msg)
	if err != nil {
		return "", fmt.Errorf("failed to send video: %w", err)
	}
	return resp.ID, nil
}

func (c *Client) SendAudio(phone string, data []byte, mimeType string, ptt bool) (string, error) {
	jid := parseJID(phone)

	uploaded, err := c.client.Upload(context.Background(), data, whatsmeow.MediaAudio)
	if err != nil {
		return "", fmt.Errorf("failed to upload audio: %w", err)
	}

	msg := &waE2E.Message{
		AudioMessage: &waE2E.AudioMessage{
			Mimetype:      proto.String(mimeType),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(data))),
			PTT:           proto.Bool(ptt),
		},
	}

	resp, err := c.client.SendMessage(context.Background(), jid, msg)
	if err != nil {
		return "", fmt.Errorf("failed to send audio: %w", err)
	}
	return resp.ID, nil
}

func (c *Client) SendDocument(phone string, data []byte, filename, mimeType string) (string, error) {
	jid := parseJID(phone)

	uploaded, err := c.client.Upload(context.Background(), data, whatsmeow.MediaDocument)
	if err != nil {
		return "", fmt.Errorf("failed to upload document: %w", err)
	}

	msg := &waE2E.Message{
		DocumentMessage: &waE2E.DocumentMessage{
			Mimetype:      proto.String(mimeType),
			FileName:      proto.String(filename),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(data))),
		},
	}

	resp, err := c.client.SendMessage(context.Background(), jid, msg)
	if err != nil {
		return "", fmt.Errorf("failed to send document: %w", err)
	}
	return resp.ID, nil
}

func (c *Client) SendSticker(phone string, data []byte, mimeType string) (string, error) {
	jid := parseJID(phone)

	uploaded, err := c.client.Upload(context.Background(), data, whatsmeow.MediaImage)
	if err != nil {
		return "", fmt.Errorf("failed to upload sticker: %w", err)
	}

	msg := &waE2E.Message{
		StickerMessage: &waE2E.StickerMessage{
			Mimetype:      proto.String(mimeType),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(data))),
		},
	}

	resp, err := c.client.SendMessage(context.Background(), jid, msg)
	if err != nil {
		return "", fmt.Errorf("failed to send sticker: %w", err)
	}
	return resp.ID, nil
}

func (c *Client) SendLocation(phone string, lat, lng float64, name, address string) (string, error) {
	jid := parseJID(phone)

	msg := &waE2E.Message{
		LocationMessage: &waE2E.LocationMessage{
			DegreesLatitude:  proto.Float64(lat),
			DegreesLongitude: proto.Float64(lng),
			Name:             proto.String(name),
			Address:          proto.String(address),
		},
	}

	resp, err := c.client.SendMessage(context.Background(), jid, msg)
	if err != nil {
		return "", fmt.Errorf("failed to send location: %w", err)
	}
	return resp.ID, nil
}

func (c *Client) SendContact(phone, contactName, contactPhone string) (string, error) {
	jid := parseJID(phone)

	vcard := fmt.Sprintf("BEGIN:VCARD\nVERSION:3.0\nFN:%s\nTEL;type=CELL;type=VOICE;waid=%s:+%s\nEND:VCARD",
		contactName, contactPhone, contactPhone)

	msg := &waE2E.Message{
		ContactMessage: &waE2E.ContactMessage{
			DisplayName: proto.String(contactName),
			Vcard:       proto.String(vcard),
		},
	}

	resp, err := c.client.SendMessage(context.Background(), jid, msg)
	if err != nil {
		return "", fmt.Errorf("failed to send contact: %w", err)
	}
	return resp.ID, nil
}

func (c *Client) SendReaction(phone, messageID, emoji string) error {
	jid := parseJID(phone)

	msg := &waE2E.Message{
		ReactionMessage: &waE2E.ReactionMessage{
			Key: &waE2E.MessageKey{
				RemoteJID: proto.String(jid.String()),
				FromMe:    proto.Bool(true),
				ID:        proto.String(messageID),
			},
			Text:              proto.String(emoji),
			SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
		},
	}

	_, err := c.client.SendMessage(context.Background(), jid, msg)
	if err != nil {
		return fmt.Errorf("failed to send reaction: %w", err)
	}
	return nil
}

func (c *Client) SendPoll(phone, question string, options []string, maxSelections int) (string, error) {
	jid := parseJID(phone)

	var pollOptions []*waE2E.PollCreationMessage_Option
	for _, opt := range options {
		pollOptions = append(pollOptions, &waE2E.PollCreationMessage_Option{
			OptionName: proto.String(opt),
		})
	}

	msg := &waE2E.Message{
		PollCreationMessage: &waE2E.PollCreationMessage{
			Name:                   proto.String(question),
			Options:                pollOptions,
			SelectableOptionsCount: proto.Uint32(uint32(maxSelections)),
		},
	}

	resp, err := c.client.SendMessage(context.Background(), jid, msg)
	if err != nil {
		return "", fmt.Errorf("failed to send poll: %w", err)
	}
	return resp.ID, nil
}

func (c *Client) CheckRegistered(phone string) (bool, error) {
	jid := parseJID(phone)
	resp, err := c.client.IsOnWhatsApp([]string{"+" + jid.User})
	if err != nil {
		return false, fmt.Errorf("failed to check registration: %w", err)
	}
	for _, r := range resp {
		if r.IsIn {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) GetProfilePicture(phone string) (string, error) {
	jid := parseJID(phone)
	pic, err := c.client.GetProfilePictureInfo(jid, &whatsmeow.GetProfilePictureParams{})
	if err != nil {
		return "", fmt.Errorf("failed to get profile picture: %w", err)
	}
	if pic == nil {
		return "", nil
	}
	return pic.URL, nil
}

func (c *Client) CreateGroup(name string, participants []string) (string, error) {
	var jids []types.JID
	for _, p := range participants {
		jids = append(jids, parseJID(p))
	}

	req := whatsmeow.ReqCreateGroup{
		Name:         name,
		Participants: jids,
	}

	info, err := c.client.CreateGroup(req)
	if err != nil {
		return "", fmt.Errorf("failed to create group: %w", err)
	}
	return info.JID.String(), nil
}

func (c *Client) GetGroupInfo(groupJID string) (*types.GroupInfo, error) {
	jid := parseGroupJID(groupJID)
	info, err := c.client.GetGroupInfo(jid)
	if err != nil {
		return nil, fmt.Errorf("failed to get group info: %w", err)
	}
	return info, nil
}

func (c *Client) GetJoinedGroups() ([]*types.GroupInfo, error) {
	groups, err := c.client.GetJoinedGroups()
	if err != nil {
		return nil, fmt.Errorf("failed to get joined groups: %w", err)
	}
	return groups, nil
}

func (c *Client) AddGroupParticipants(groupJID string, participants []string) error {
	jid := parseGroupJID(groupJID)
	var jids []types.JID
	for _, p := range participants {
		jids = append(jids, parseJID(p))
	}

	_, err := c.client.UpdateGroupParticipants(jid, jids, whatsmeow.ParticipantChangeAdd)
	if err != nil {
		return fmt.Errorf("failed to add participants: %w", err)
	}
	return nil
}

func (c *Client) RemoveGroupParticipants(groupJID string, participants []string) error {
	jid := parseGroupJID(groupJID)
	var jids []types.JID
	for _, p := range participants {
		jids = append(jids, parseJID(p))
	}

	_, err := c.client.UpdateGroupParticipants(jid, jids, whatsmeow.ParticipantChangeRemove)
	if err != nil {
		return fmt.Errorf("failed to remove participants: %w", err)
	}
	return nil
}

func (c *Client) LeaveGroup(groupJID string) error {
	jid := parseGroupJID(groupJID)
	return c.client.LeaveGroup(jid)
}

func (c *Client) GetGroupInviteLink(groupJID string) (string, error) {
	jid := parseGroupJID(groupJID)
	link, err := c.client.GetGroupInviteLink(jid, false)
	if err != nil {
		return "", fmt.Errorf("failed to get invite link: %w", err)
	}
	return link, nil
}

func parseJID(phone string) types.JID {
	phone = strings.NewReplacer("+", "", " ", "", "-", "", "(", "", ")", "").Replace(phone)
	return types.NewJID(phone, types.DefaultUserServer)
}

func parseGroupJID(jid string) types.JID {
	jid = strings.TrimSuffix(jid, "@g.us")
	return types.NewJID(jid, types.GroupServer)
}

func DownloadMedia(mediaURL string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(mediaURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return data, nil
}

func DecodeBase64(data string) ([]byte, error) {
	if idx := strings.Index(data, ","); idx != -1 {
		data = data[idx+1:]
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}
	return decoded, nil
}
