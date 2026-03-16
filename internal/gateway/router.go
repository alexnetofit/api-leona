package gateway

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"github.com/alexnetofit/api-leona/configs"
	"github.com/alexnetofit/api-leona/internal/handlers"
	"github.com/alexnetofit/api-leona/internal/middleware"
	"github.com/alexnetofit/api-leona/internal/services"
	"github.com/alexnetofit/api-leona/pkg/whatsapp"
)

func SetupRouter(
	cfg *configs.Config,
	manager *whatsapp.Manager,
	db *services.DB,
	proxyManager *services.ProxyManager,
) *fiber.App {
	app := fiber.New(fiber.Config{
		BodyLimit: cfg.Server.BodyLimitMB * 1024 * 1024,
	})

	app.Use(recover.New())
	app.Use(logger.New())
	app.Use(cors.New())

	instanceH := handlers.NewInstanceHandler(manager, db, proxyManager)
	messageH := handlers.NewMessageHandler(manager)
	groupH := handlers.NewGroupHandler(manager)
	contactH := handlers.NewContactHandler(manager)
	adminH := handlers.NewAdminHandler(manager, proxyManager)

	// Health (no auth)
	app.Get("/health", adminH.Health)

	// All other routes require auth
	api := app.Group("/api", middleware.Auth(cfg), middleware.RateLimit(60))

	// Instance management
	api.Post("/instance/create", instanceH.Create)
	api.Get("/instance/list", instanceH.List)
	api.Get("/instance/:id/info", instanceH.Info)
	api.Post("/instance/:id/connect", instanceH.Connect)
	api.Post("/instance/:id/disconnect", instanceH.Disconnect)
	api.Post("/instance/:id/logout", instanceH.Logout)
	api.Delete("/instance/:id", instanceH.Delete)
	api.Put("/instance/:id/webhook", instanceH.SetWebhook)
	api.Get("/instance/:id/qrcode", instanceH.GetQRCode)
	api.Get("/instance/:id/qrcode/image", instanceH.GetQRCodeImage)
	api.Post("/instance/:id/restart", instanceH.Restart)

	// Messages
	api.Post("/:id/send/text", messageH.SendText)
	api.Post("/:id/send/image", messageH.SendImage)
	api.Post("/:id/send/video", messageH.SendVideo)
	api.Post("/:id/send/audio", messageH.SendAudio)
	api.Post("/:id/send/document", messageH.SendDocument)
	api.Post("/:id/send/sticker", messageH.SendSticker)
	api.Post("/:id/send/location", messageH.SendLocation)
	api.Post("/:id/send/contact", messageH.SendContact)
	api.Post("/:id/send/reaction", messageH.SendReaction)
	api.Post("/:id/send/poll", messageH.SendPoll)

	// Chat
	api.Post("/:id/chat/presence", messageH.SendPresence)
	api.Post("/:id/chat/read", messageH.MarkRead)

	// Groups
	api.Post("/:id/group/create", groupH.Create)
	api.Get("/:id/group/list", groupH.List)
	api.Get("/:id/group/:groupJID/info", groupH.Info)
	api.Post("/:id/group/:groupJID/participants/add", groupH.AddParticipants)
	api.Post("/:id/group/:groupJID/participants/remove", groupH.RemoveParticipants)
	api.Get("/:id/group/:groupJID/invite-link", groupH.GetInviteLink)
	api.Post("/:id/group/:groupJID/leave", groupH.Leave)

	// Contacts
	api.Post("/:id/contact/check", contactH.Check)
	api.Get("/:id/contact/:phone/picture", contactH.GetPicture)
	api.Get("/:id/contact/:phone/status", contactH.GetStatus)

	// Admin
	api.Get("/admin/proxies", adminH.ProxiesStatus)
	api.Get("/admin/proxies/:ip/health", adminH.ProxyHealth)
	api.Post("/admin/proxies/:ip/replace", adminH.ReplaceProxy)
	api.Get("/admin/stats", adminH.Stats)

	return app
}
