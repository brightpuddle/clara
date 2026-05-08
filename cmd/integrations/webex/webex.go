package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/brightpuddle/clara/cmd/integrations/webex/internal/webexapi"
	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/rs/zerolog/log"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
)

const description = "Webex integration: read and reply to messages (user account) and send bot notifications."

type Webex struct {
	cfg      webexapi.Config
	tokenMgr *webexapi.TokenManager
	user     *webexapi.Client
	bot      *webexapi.Client
	router   *webexapi.Router
	eventCh  chan contract.Event
}

func newWebex() *Webex {
	return &Webex{
		router:  webexapi.NewRouter(),
		eventCh: make(chan contract.Event, 64),
	}
}

func (w *Webex) Configure(raw []byte) error {
	if len(raw) == 0 {
		return errors.New("webex: no configuration provided")
	}
	if err := json.Unmarshal(raw, &w.cfg); err != nil {
		return errors.Wrap(err, "webex: unmarshal config")
	}

	if w.cfg.UserEnabled() {
		tm, err := webexapi.NewTokenManager(w.cfg.ClientID, w.cfg.ClientSecret)
		if err != nil {
			return fmt.Errorf("webex: init token manager: %w", err)
		}
		w.tokenMgr = tm
		w.user = webexapi.NewUserClient(tm)

		if tm.HasToken() {
			go w.ensureUserWebhook()
		}
	}

	if w.cfg.BotEnabled() {
		w.bot = webexapi.NewBotClient(w.cfg.BotToken)
		go w.ensureBotWebhook()
	}

	// We subscribe to router events and forward them to Clara
	go func() {
		events, cancel := w.router.Subscribe("local")
		defer cancel()
		for ev := range events {
			var params map[string]string
			if err := json.Unmarshal(ev.Data, &params); err == nil {
				// Convert to Clara contract.Event
				dataBytes, _ := json.Marshal(params)
				w.eventCh <- contract.Event{
					Name: ev.Type,
					Data: dataBytes,
				}
			}
		}
	}()

	return nil
}

func (w *Webex) Description() (string, error) { return description, nil }

func (w *Webex) Tools() ([]byte, error) {
	tools := []mcp.Tool{
		mcp.NewTool(
			"rooms.list",
			mcp.WithDescription("List the user's Webex spaces and direct conversations, sorted by most recent activity."),
			mcp.WithNumber("max", mcp.Description("Maximum number of rooms to return (default 50).")),
		),
		mcp.NewTool(
			"messages.list",
			mcp.WithDescription("List messages in a Webex room (newest first) using the user account."),
			mcp.WithString("room_id", mcp.Required(), mcp.Description("Webex room/space ID.")),
			mcp.WithNumber("max", mcp.Description("Maximum number of messages to return (default 50).")),
		),
		mcp.NewTool(
			"message.get",
			mcp.WithDescription("Retrieve a single Webex message by ID (includes full text)."),
			mcp.WithString("message_id", mcp.Required(), mcp.Description("Webex message ID.")),
		),
		mcp.NewTool(
			"message.reply",
			mcp.WithDescription("Send a reply to a Webex room using the user account."),
			mcp.WithString("room_id", mcp.Required(), mcp.Description("Webex room/space ID to post into.")),
			mcp.WithString("text", mcp.Required(), mcp.Description("Plain-text message body.")),
		),
		mcp.NewTool(
			"notification.send",
			mcp.WithDescription("Send a notification message to a Webex room via the bot account."),
			mcp.WithString("room_id", mcp.Required(), mcp.Description("Webex room/space ID to post into.")),
			mcp.WithString("text", mcp.Required(), mcp.Description("Notification message text.")),
		),
		mcp.NewTool(
			"approval.request",
			mcp.WithDescription(
				"Post an approval card with Approve/Reject buttons and block until decided. "+
					"Returns \"approved\", \"rejected\", or \"timeout\".",
			),
			mcp.WithString("room_id", mcp.Required(), mcp.Description("Target Webex room ID.")),
			mcp.WithString("title", mcp.Required(), mcp.Description("Short title for the approval card.")),
			mcp.WithString("description", mcp.Description("Detail shown in the card body.")),
			mcp.WithNumber("timeout_s", mcp.Description("Seconds to wait for decision (default 300).")),
		),
		mcp.NewTool(
			"message_created",
			mcp.WithDescription(
				"Event source: fired when a new Webex message is received (via webhook callback). "+
					"Use as a trigger: clara.on(webex.message_created). "+
					"Event data: {message_id, room_id, room_type, person_email, person_id, text, created}.",
			),
		),
	}
	return json.Marshal(tools)
}

func (w *Webex) CallTool(name string, args []byte) ([]byte, error) {
	switch name {
	case "rooms.list":
		return w.callRoomsList(args)
	case "messages.list":
		return w.callMessagesList(args)
	case "message.get":
		return w.callMessageGet(args)
	case "message.reply":
		return w.callMessageReply(args)
	case "notification.send":
		return w.callNotificationSend(args)
	case "approval.request":
		return w.callApprovalRequest(args)
	case "message_created":
		return json.Marshal(map[string]string{"error": "message_created is an event source, not a callable tool"})
	default:
		return nil, errors.Newf("webex: unknown tool %q", name)
	}
}

// --- tool implementations ---

func (w *Webex) callRoomsList(args []byte) ([]byte, error) {
	if w.user == nil {
		return nil, errors.New("webex user account not configured")
	}
	var a struct {
		Max float64 `json:"max"`
	}
	_ = json.Unmarshal(args, &a)
	max := int(a.Max)
	if max <= 0 {
		max = 50
	}
	rooms, err := w.user.ListRooms(max)
	if err != nil {
		return nil, err
	}
	return json.Marshal(rooms)
}

func (w *Webex) callMessagesList(args []byte) ([]byte, error) {
	if w.user == nil {
		return nil, errors.New("webex user account not configured")
	}
	var a struct {
		RoomID string  `json:"room_id"`
		Max    float64 `json:"max"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "unmarshal args")
	}
	if a.RoomID == "" {
		return nil, errors.New("room_id required")
	}
	max := int(a.Max)
	if max <= 0 {
		max = 50
	}
	msgs, err := w.user.ListMessages(a.RoomID, max)
	if err != nil {
		return nil, err
	}
	return json.Marshal(msgs)
}

func (w *Webex) callMessageGet(args []byte) ([]byte, error) {
	if w.user == nil {
		return nil, errors.New("webex user account not configured")
	}
	var a struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "unmarshal args")
	}
	if a.MessageID == "" {
		return nil, errors.New("message_id required")
	}
	msg, err := w.user.GetMessage(a.MessageID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(msg)
}

func (w *Webex) callMessageReply(args []byte) ([]byte, error) {
	if w.user == nil {
		return nil, errors.New("webex user account not configured")
	}
	var a struct {
		RoomID string `json:"room_id"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "unmarshal args")
	}
	if a.RoomID == "" || a.Text == "" {
		return nil, errors.New("room_id and text required")
	}
	msg, err := w.user.SendMessage(a.RoomID, a.Text)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"message_id": msg.ID})
}

func (w *Webex) callNotificationSend(args []byte) ([]byte, error) {
	if w.bot == nil {
		return nil, errors.New("webex bot account not configured")
	}
	var a struct {
		RoomID string `json:"room_id"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "unmarshal args")
	}
	if a.RoomID == "" || a.Text == "" {
		return nil, errors.New("room_id and text required")
	}
	msg, err := w.bot.SendMessage(a.RoomID, a.Text)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"message_id": msg.ID})
}

func (w *Webex) callApprovalRequest(args []byte) ([]byte, error) {
	if w.bot == nil {
		return nil, errors.New("webex bot account not configured")
	}
	var a struct {
		Title       string  `json:"title"`
		Description string  `json:"description"`
		RoomID      string  `json:"room_id"`
		TimeoutS    float64 `json:"timeout_s"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, errors.Wrap(err, "unmarshal args")
	}
	if a.RoomID == "" {
		return nil, errors.New("room_id required")
	}
	timeoutSec := int(a.TimeoutS)
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	requestID := uuid.New().String()
	
	w.router.RegisterApproval(requestID)
	_, err := w.bot.SendApproval(a.RoomID, "local", requestID, a.Title, a.Description)
	if err != nil {
		return nil, errors.Wrap(err, "send approval")
	}

	waitCh := w.router.GetApprovalChan(requestID)
	if waitCh == nil {
		return nil, errors.New("approval not found")
	}

	decision, ok := w.router.WaitApproval(requestID, waitCh, time.Duration(timeoutSec)*time.Second)
	if !ok {
		return json.Marshal(map[string]string{"decision": "timeout"})
	}
	return json.Marshal(map[string]string{"decision": decision.Decision})
}

// --- HTTPIntegration implementation ---

func (w *Webex) HandleHTTP(method, path string, headers map[string]string, body []byte) (int, []byte, error) {
	if method == http.MethodGet && strings.HasPrefix(path, "/auth/webex") {
		return w.handleOAuthCallback(path)
	}
	if method == http.MethodPost && strings.HasPrefix(path, "/api/webex/callback") {
		return w.handleIncomingWebhook(headers, body)
	}
	return 404, []byte("Not Found"), nil
}

func (w *Webex) handleOAuthCallback(path string) (int, []byte, error) {
	u, err := url.Parse(path)
	if err != nil {
		return 400, []byte("Invalid path"), nil
	}
	q := u.Query()
	
	if errCode := q.Get("error"); errCode != "" {
		desc := q.Get("error_description")
		log.Warn().Str("error", errCode).Str("desc", desc).Msg("webex: OAuth denied")
		return 400, []byte("<p>x Webex authorization denied: " + desc + "</p>"), nil
	}

	code := q.Get("code")
	if code == "" {
		return 400, []byte("<p>x Missing authorization code.</p>"), nil
	}

	if w.tokenMgr == nil {
		return 503, []byte("webex OAuth not configured"), nil
	}

	tr, err := webexapi.ExchangeCode(w.cfg.ClientID, w.cfg.ClientSecret, code, w.cfg.OAuthRedirectURI())
	if err != nil {
		log.Error().Err(err).Msg("webex: code exchange failed")
		return 500, []byte("token exchange failed"), nil
	}

	if err := w.tokenMgr.Store(tr.AccessToken, tr.RefreshToken, tr.ExpiresIn); err != nil {
		log.Error().Err(err).Msg("webex: failed to persist tokens")
		return 500, []byte("token storage failed"), nil
	}

	log.Info().Msg("webex: OAuth flow complete - tokens stored")
	go w.ensureUserWebhook()

	return 200, []byte("<p>v Webex authorized successfully. You can close this tab.</p>"), nil
}

func (w *Webex) handleIncomingWebhook(headers map[string]string, body []byte) (int, []byte, error) {
	if w.cfg.WebhookSecret != "" {
		// HTTP headers are case-insensitive, but we get them exactly as parsed
		var sig string
		for k, v := range headers {
			if strings.EqualFold(k, "X-Spark-Signature") {
				sig = v
				break
			}
		}
		if !verifySignature(w.cfg.WebhookSecret, body, sig) {
			log.Warn().Str("sig", sig).Msg("webex: invalid webhook signature")
			return 401, []byte("invalid signature"), nil
		}
	}

	var payload struct {
		Resource string `json:"resource"`
		Event    string `json:"event"`
		Data     struct {
			ID          string `json:"id"`
			RoomID      string `json:"roomId"`
			RoomType    string `json:"roomType"`
			PersonEmail string `json:"personEmail"`
			PersonID    string `json:"personId"`
			Created     string `json:"created"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 400, []byte("invalid JSON"), nil
	}

	log.Info().
		Str("resource", payload.Resource).
		Str("event", payload.Event).
		Str("message_id", payload.Data.ID).
		Str("room_id", payload.Data.RoomID).
		Msg("webex webhook received")

	if payload.Resource == "attachmentActions" && payload.Event == "created" {
		w.handleAttachmentAction(payload.Data.ID)
		return 200, []byte(""), nil
	}

	if payload.Resource != "messages" || payload.Event != "created" || payload.Data.ID == "" {
		return 200, []byte(""), nil
	}

	var msgText string
	if w.user != nil {
		if msg, err := w.user.GetMessage(payload.Data.ID); err == nil {
			msgText = msg.Text
		}
	}

	evData, _ := json.Marshal(map[string]string{
		"message_id":   payload.Data.ID,
		"room_id":      payload.Data.RoomID,
		"room_type":    payload.Data.RoomType,
		"person_email": payload.Data.PersonEmail,
		"person_id":    payload.Data.PersonID,
		"text":         msgText,
		"created":      payload.Data.Created,
	})
	w.router.Publish(webexapi.Event{Type: "message_created", Data: evData})

	return 200, []byte(""), nil
}

func (w *Webex) handleAttachmentAction(id string) {
	client := w.user
	if client == nil {
		client = w.bot
	}
	if client == nil {
		return
	}

	action, err := client.GetAttachmentAction(id)
	if err != nil {
		log.Warn().Err(err).Str("action_id", id).Msg("webex: could not fetch attachment action")
		return
	}

	requestID, _ := action.Inputs["request_id"].(string)
	decision, _ := action.Inputs["decision"].(string)

	if requestID != "" && decision != "" {
		log.Info().Str("request_id", requestID).Str("decision", decision).Msg("webex: approval resolved")
		w.router.ResolveApproval(requestID, webexapi.ApprovalDecision{
			Decision: decision,
			User:     action.PersonID,
		})
	}
}

func (w *Webex) ensureUserWebhook() {
	if w.tokenMgr == nil || w.cfg.WebhookCallbackURL() == "" {
		return
	}
	token, err := w.tokenMgr.AccessToken()
	if err != nil {
		log.Warn().Err(err).Msg("webex: cannot get token for user webhook")
		return
	}
	callbackURL := w.cfg.WebhookCallbackURL()
	if err := webexapi.EnsureWebhook(token, callbackURL, w.cfg.WebhookSecret, "clara-user-messages", "messages"); err != nil {
		log.Warn().Err(err).Str("url", callbackURL).Msg("webex: user webhook registration failed")
	}
}

func (w *Webex) ensureBotWebhook() {
	if !w.cfg.BotEnabled() || w.cfg.WebhookCallbackURL() == "" {
		return
	}
	callbackURL := w.cfg.WebhookCallbackURL()
	if err := webexapi.EnsureWebhook(w.cfg.BotToken, callbackURL, w.cfg.WebhookSecret, "clara-bot-approvals", "attachmentActions"); err != nil {
		log.Warn().Err(err).Str("url", callbackURL).Msg("webex: bot webhook registration failed")
	}
}

func verifySignature(secret string, body []byte, signature string) bool {
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// EventStreamer implementation
func (w *Webex) StreamEvents() (<-chan contract.Event, error) {
	return w.eventCh, nil
}
