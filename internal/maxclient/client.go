package maxclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const apiBase = "https://platform-api.max.ru"

// Client — минимальный HTTP-клиент MAX Bot API (/me, /updates, /messages, /answers).
type Client struct {
	token string
	http  *http.Client
}

func New(token string) *Client {
	return &Client{
		token: token,
		// Таймаут HTTP-клиента должен быть БОЛЬШЕ чем long-polling timeout,
		// иначе HTTP-клиент обрубит соединение раньше, чем сервер ответит.
		http: &http.Client{Timeout: 120 * time.Second},
	}
}

// ===== Типы MAX API =====

type Bot struct {
	UserID   int64  `json:"user_id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	IsBot    bool   `json:"is_bot"`
}

type User struct {
	UserID   int64  `json:"user_id"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
}

type Recipient struct {
	UserID   int64  `json:"user_id,omitempty"`
	ChatID   int64  `json:"chat_id,omitempty"`
	ChatType string `json:"chat_type"`
}

type MessageBody struct {
	MID  string `json:"mid"`
	Seq  int64  `json:"seq"`
	Text string `json:"text"`
}

type Message struct {
	Sender    *User        `json:"sender,omitempty"`
	Recipient *Recipient   `json:"recipient,omitempty"`
	Timestamp int64        `json:"timestamp"`
	Body      *MessageBody `json:"body,omitempty"`
}

// Callback is sent when a user presses an inline callback button.
// Field names match max-bot-api-client-go/schemes.Callback.
type Callback struct {
	Timestamp  int64  `json:"timestamp"`
	CallbackID string `json:"callback_id"`
	Payload    string `json:"payload,omitempty"`
	User       *User  `json:"user,omitempty"`
}

// Update — универсальный объект обновления.
type Update struct {
	UpdateType string    `json:"update_type"`
	Timestamp  int64     `json:"timestamp"`
	ChatID     int64     `json:"chat_id,omitempty"` // bot_started
	User       *User     `json:"user,omitempty"`    // bot_started
	Payload    string    `json:"payload,omitempty"` // bot_started — значение ?start=... из диплинка
	Message    *Message  `json:"message,omitempty"` // message_created, message_callback
	Callback   *Callback `json:"callback,omitempty"`
}

type UpdatesResponse struct {
	Updates []Update `json:"updates"`
	Marker  int64    `json:"marker"`
}

// ===== Методы =====

// GetMe — ping на старте, чтобы проверить токен.
func (c *Client) GetMe(ctx context.Context) (*Bot, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiBase+"/me", nil)
	if err != nil {
		return nil, err
	}
	var bot Bot
	if err := c.do(req, &bot); err != nil {
		return nil, err
	}
	return &bot, nil
}

// GetUpdates — long polling.
// marker=0 означает «все неподтверждённые», иначе сервер вернёт только новые.
// timeout — сколько секунд держать соединение (MAX принимает до 90).
func (c *Client) GetUpdates(ctx context.Context, marker int64, timeout int) (*UpdatesResponse, error) {
	q := url.Values{}
	q.Set("timeout", strconv.Itoa(timeout))
	q.Set("limit", "100")
	if marker > 0 {
		q.Set("marker", strconv.FormatInt(marker, 10))
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiBase+"/updates?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}

	var resp UpdatesResponse
	if err := c.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type sendMessageBody struct {
	Text string `json:"text"`
}

// SendMessage sends text to a private chat by chat_id.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	return c.SendMessageWithKeyboard(ctx, chatID, false, text, nil)
}

// SendToUser sends text by user_id (cold outreach / reminders).
func (c *Client) SendToUser(ctx context.Context, userID int64, text string) error {
	return c.SendMessageWithKeyboard(ctx, userID, true, text, nil)
}

// SendMessageWithKeyboard posts a message with optional inline keyboard.
// When rows is nil/empty, only text is sent. Authorization: token without Bearer.
func (c *Client) SendMessageWithKeyboard(ctx context.Context, recipientID int64, byUserID bool, text string, rows [][]CallbackButton) error {
	body, err := marshalOutboundBody(text, rows)
	if err != nil {
		return err
	}

	q := url.Values{}
	if byUserID {
		q.Set("user_id", strconv.FormatInt(recipientID, 10))
	} else {
		q.Set("chat_id", strconv.FormatInt(recipientID, 10))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		apiBase+"/messages?"+q.Encode(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, nil)
}

// AnswerCallback acknowledges a button press (POST /answers?callback_id=...).
func (c *Client) AnswerCallback(ctx context.Context, callbackID, notification string) error {
	if callbackID == "" {
		return fmt.Errorf("callback_id is empty")
	}
	body, err := json.Marshal(callbackAnswerBody{Notification: notification})
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("callback_id", callbackID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		apiBase+"/answers?"+q.Encode(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, nil)
}

// do — общий ход: авторизация, запрос, чтение тела, разбор ошибок.
// ВАЖНО: MAX ждёт заголовок "Authorization: <token>" БЕЗ префикса "Bearer".
func (c *Client) do(req *http.Request, out interface{}) error {
	req.Header.Set("Authorization", c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("чтение тела: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("MAX API %d: %s", resp.StatusCode, string(data))
	}

	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("парсинг JSON: %w; body: %s", err, string(data))
		}
	}
	return nil
}
