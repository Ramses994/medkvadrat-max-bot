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

// Client — минимальный HTTP-клиент MAX Bot API.
// Реализованы только три метода: /me, /updates, /messages.
// Остальное (клавиатуры, вложения, чаты) добавим по мере необходимости.
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

// Update — универсальный объект обновления.
// Набор полей зависит от update_type; мы используем только bot_started и message_created.
type Update struct {
	UpdateType string   `json:"update_type"`
	Timestamp  int64    `json:"timestamp"`
	ChatID     int64    `json:"chat_id,omitempty"` // bot_started
	User       *User    `json:"user,omitempty"`    // bot_started
	Payload    string   `json:"payload,omitempty"` // bot_started — значение ?start=... из диплинка
	Message    *Message `json:"message,omitempty"` // message_created
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

// SendMessage — отправка текста в диалог (private).
// chatID для bot_started берём из update.ChatID, для message_created — из extractChatID.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	body, _ := json.Marshal(sendMessageBody{Text: text})

	q := url.Values{}
	q.Set("chat_id", strconv.FormatInt(chatID, 10))

	req, err := http.NewRequestWithContext(ctx, "POST",
		apiBase+"/messages?"+q.Encode(), bytes.NewReader(body))
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
