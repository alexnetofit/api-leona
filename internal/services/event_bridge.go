package services

import (
	"encoding/json"
	"log"
	"time"

	"go.mau.fi/whatsmeow/types/events"
)

type WebhookEvent struct {
	Event      string      `json:"event"`
	InstanceID string      `json:"instance_id"`
	Timestamp  int64       `json:"timestamp"`
	Data       interface{} `json:"data"`
}

type EventBridge struct {
	webhookDispatcher *WebhookDispatcher
	db                *DB
}

func NewEventBridge(dispatcher *WebhookDispatcher, db *DB) *EventBridge {
	return &EventBridge{
		webhookDispatcher: dispatcher,
		db:                db,
	}
}

func (eb *EventBridge) HandleEvent(instanceID string, evt interface{}) {
	var webhookEvt *WebhookEvent

	// The WhatsApp client sends map[string]interface{} with "type" and "event" keys
	m, ok := evt.(map[string]interface{})
	if !ok {
		return
	}

	eventType, _ := m["type"].(string)
	rawEvent := m["event"]

	switch eventType {
	case "message.received":
		if msg, ok := rawEvent.(*events.Message); ok {
			webhookEvt = eb.handleMessage(instanceID, msg)
		}
	case "message.delivered", "message.read", "message.played":
		if receipt, ok := rawEvent.(*events.Receipt); ok {
			webhookEvt = eb.handleReceipt(instanceID, receipt, eventType)
		}
	case "connection.connected":
		log.Printf("[EventBridge] instance %s connected!", instanceID)
		_ = eb.db.UpdateInstanceStatus(instanceID, "connected")
		webhookEvt = &WebhookEvent{
			Event:      "connection.connected",
			InstanceID: instanceID,
			Timestamp:  time.Now().Unix(),
			Data:       map[string]interface{}{},
		}
	case "connection.disconnected":
		log.Printf("[EventBridge] instance %s disconnected", instanceID)
		_ = eb.db.UpdateInstanceStatus(instanceID, "disconnected")
		webhookEvt = &WebhookEvent{
			Event:      "connection.disconnected",
			InstanceID: instanceID,
			Timestamp:  time.Now().Unix(),
			Data:       map[string]interface{}{},
		}
	case "connection.logged_out":
		log.Printf("[EventBridge] instance %s logged out", instanceID)
		_ = eb.db.UpdateInstanceStatus(instanceID, "logged_out")
		webhookEvt = &WebhookEvent{
			Event:      "connection.logged_out",
			InstanceID: instanceID,
			Timestamp:  time.Now().Unix(),
			Data:       map[string]interface{}{},
		}
	case "connection.qrcode":
		qr, _ := m["qr"].(string)
		if qr != "" {
			webhookEvt = &WebhookEvent{
				Event:      "connection.qrcode",
				InstanceID: instanceID,
				Timestamp:  time.Now().Unix(),
				Data:       map[string]interface{}{"qr_code": qr},
			}
		}
		if v, ok := rawEvent.(*events.QR); ok && len(v.Codes) > 0 {
			webhookEvt = &WebhookEvent{
				Event:      "connection.qrcode",
				InstanceID: instanceID,
				Timestamp:  time.Now().Unix(),
				Data:       map[string]interface{}{"qr_code": v.Codes[0]},
			}
		}
	case "group.updated":
		if v, ok := rawEvent.(*events.GroupInfo); ok {
			webhookEvt = &WebhookEvent{
				Event:      "group.updated",
				InstanceID: instanceID,
				Timestamp:  time.Now().Unix(),
				Data:       map[string]interface{}{"group_jid": v.JID.String()},
			}
		}
	case "call.offer":
		if v, ok := rawEvent.(*events.CallOffer); ok {
			webhookEvt = &WebhookEvent{
				Event:      "call.offer",
				InstanceID: instanceID,
				Timestamp:  time.Now().Unix(),
				Data: map[string]interface{}{
					"from":    v.From.String(),
					"call_id": v.CallID,
				},
			}
		}
	case "presence.update":
		if v, ok := rawEvent.(*events.Presence); ok {
			webhookEvt = &WebhookEvent{
				Event:      "presence.update",
				InstanceID: instanceID,
				Timestamp:  time.Now().Unix(),
				Data: map[string]interface{}{
					"from":      v.From.String(),
					"available": !v.Unavailable,
				},
			}
		}
	default:
		return
	}

	if webhookEvt != nil {
		eb.dispatch(webhookEvt)
	}
}

func (eb *EventBridge) handleMessage(instanceID string, msg *events.Message) *WebhookEvent {
	info := msg.Info

	msgType := "text"
	body := ""

	if msg.Message != nil {
		if msg.Message.GetConversation() != "" {
			body = msg.Message.GetConversation()
		} else if msg.Message.GetExtendedTextMessage() != nil {
			body = msg.Message.GetExtendedTextMessage().GetText()
		} else if msg.Message.GetImageMessage() != nil {
			msgType = "image"
			body = msg.Message.GetImageMessage().GetCaption()
		} else if msg.Message.GetVideoMessage() != nil {
			msgType = "video"
			body = msg.Message.GetVideoMessage().GetCaption()
		} else if msg.Message.GetAudioMessage() != nil {
			msgType = "audio"
		} else if msg.Message.GetDocumentMessage() != nil {
			msgType = "document"
			body = msg.Message.GetDocumentMessage().GetFileName()
		} else if msg.Message.GetStickerMessage() != nil {
			msgType = "sticker"
		} else if msg.Message.GetLocationMessage() != nil {
			msgType = "location"
		} else if msg.Message.GetContactMessage() != nil {
			msgType = "contact"
			body = msg.Message.GetContactMessage().GetDisplayName()
		} else if msg.Message.GetReactionMessage() != nil {
			msgType = "reaction"
			body = msg.Message.GetReactionMessage().GetText()
		} else if msg.Message.GetPollCreationMessage() != nil {
			msgType = "poll"
			body = msg.Message.GetPollCreationMessage().GetName()
		}
	}

	return &WebhookEvent{
		Event:      "message.received",
		InstanceID: instanceID,
		Timestamp:  info.Timestamp.Unix(),
		Data: map[string]interface{}{
			"from":         info.Sender.User,
			"name":         info.PushName,
			"message_id":   info.ID,
			"message_type": msgType,
			"body":         body,
			"is_group":     info.IsGroup,
			"timestamp":    info.Timestamp.Unix(),
		},
	}
}

func (eb *EventBridge) handleReceipt(instanceID string, receipt *events.Receipt, eventType string) *WebhookEvent {
	messageIDs := make([]string, len(receipt.MessageIDs))
	copy(messageIDs, receipt.MessageIDs)

	return &WebhookEvent{
		Event:      eventType,
		InstanceID: instanceID,
		Timestamp:  receipt.Timestamp.Unix(),
		Data: map[string]interface{}{
			"from":        receipt.MessageSource.Sender.User,
			"message_ids": messageIDs,
		},
	}
}

func (eb *EventBridge) dispatch(evt *WebhookEvent) {
	payload, err := json.Marshal(evt)
	if err != nil {
		log.Printf("[EventBridge] marshal error: %v", err)
		return
	}

	instance, err := eb.db.GetInstance(evt.InstanceID)
	if err != nil {
		log.Printf("[EventBridge] get instance error: %v", err)
	}

	webhookURL := ""
	if instance != nil && instance.WebhookURL != "" {
		webhookURL = instance.WebhookURL
	}

	eb.webhookDispatcher.Dispatch(evt.InstanceID, evt.Event, webhookURL, payload)
}
