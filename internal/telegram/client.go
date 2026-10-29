package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultEndpoint is the public Bot API server.
const DefaultEndpoint = "https://api.telegram.org"

// Error is an error returned by the Bot API ("ok": false).
type Error struct {
	Method      string
	Code        int
	Description string
	RetryAfter  int // seconds, set on 429 Too Many Requests
}

func (e *Error) Error() string {
	return fmt.Sprintf("telegram %s: %d %s", e.Method, e.Code, e.Description)
}

// IsNotModified reports whether err is the harmless "message is not
// modified" error returned when an edit would not change anything.
func IsNotModified(err error) bool {
	var e *Error
	return errors.As(err, &e) && strings.Contains(e.Description, "message is not modified")
}

// Client calls the Bot API for one bot token. It is safe for concurrent use.
type Client struct {
	token    string
	endpoint string
	http     *http.Client
	Debug    bool
}

// New returns a client for the public Bot API.
func New(token string) *Client { return NewWithEndpoint(token, DefaultEndpoint) }

// NewWithEndpoint returns a client for a custom Bot API server (or a test
// server); endpoint is e.g. "https://api.telegram.org".
func NewWithEndpoint(token, endpoint string) *Client {
	return &Client{
		token:    token,
		endpoint: strings.TrimRight(endpoint, "/"),
		// Long polling sets its own, longer deadline through the context.
		http: &http.Client{Timeout: 0},
	}
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

const defaultTimeout = 30 * time.Second

// Call invokes method with JSON params and decodes the result into out
// (which may be nil). A 429 response is retried once after the delay the
// server asks for, up to 30 seconds.
func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.do(ctx, method, "application/json", func() io.Reader { return bytes.NewReader(body) }, out)
}

func (c *Client) do(ctx context.Context, method, contentType string, body func() io.Reader, out any) error {
	for attempt := 0; ; attempt++ {
		err := c.once(ctx, method, contentType, body(), out)
		var apiErr *Error
		if attempt == 0 && errors.As(err, &apiErr) && apiErr.Code == http.StatusTooManyRequests &&
			apiErr.RetryAfter > 0 && apiErr.RetryAfter <= 30 {
			select {
			case <-time.After(time.Duration(apiErr.RetryAfter) * time.Second):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return err
	}
}

func (c *Client) once(ctx context.Context, method, contentType string, body io.Reader, out any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultTimeout)
		defer cancel()
	}
	url := c.endpoint + "/bot" + c.token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http.Do(req)
	if err != nil {
		// Never leak the token through the URL in error messages.
		return fmt.Errorf("telegram %s: %w", method, redact(err, c.token))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	if c.Debug {
		log.Printf("telegram %s -> %s", method, raw)
	}
	var r apiResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("telegram %s: %s: %w", method, resp.Status, err)
	}
	if !r.OK {
		return &Error{Method: method, Code: r.ErrorCode, Description: r.Description, RetryAfter: r.Parameters.RetryAfter}
	}
	if out != nil {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return fmt.Errorf("telegram %s: decode result: %w", method, err)
		}
	}
	return nil
}

type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

func redact(err error, token string) error {
	if token == "" || !strings.Contains(err.Error(), token) {
		return err
	}
	return &redactedError{msg: strings.ReplaceAll(err.Error(), token, "<token>"), err: err}
}

// GetMe returns the bot's own user.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var u User
	return &u, c.Call(ctx, "getMe", struct{}{}, &u)
}

// SendMessageParams are the options for SendMessage.
type SendMessageParams struct {
	ChatID                int64                 `json:"chat_id"`
	Text                  string                `json:"text"`
	ParseMode             string                `json:"parse_mode,omitempty"`
	DisableWebPagePreview bool                  `json:"disable_web_page_preview,omitempty"`
	ReplyMarkup           *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

// SendMessage sends a text message.
func (c *Client) SendMessage(ctx context.Context, p SendMessageParams) (*Message, error) {
	var m Message
	return &m, c.Call(ctx, "sendMessage", p, &m)
}

// EditMessageTextParams are the options for EditMessageText.
type EditMessageTextParams struct {
	ChatID                int64                 `json:"chat_id"`
	MessageID             int                   `json:"message_id"`
	Text                  string                `json:"text"`
	ParseMode             string                `json:"parse_mode,omitempty"`
	DisableWebPagePreview bool                  `json:"disable_web_page_preview,omitempty"`
	ReplyMarkup           *InlineKeyboardMarkup `json:"reply_markup,omitempty"`
}

// EditMessageText replaces the text (and keyboard) of a sent message.
func (c *Client) EditMessageText(ctx context.Context, p EditMessageTextParams) error {
	// The result is a Message or `true`; neither is needed.
	return c.Call(ctx, "editMessageText", p, nil)
}

// AnswerCallbackQuery acknowledges a button press, optionally with a toast
// (or an alert pop-up when showAlert is true).
func (c *Client) AnswerCallbackQuery(ctx context.Context, id, text string, showAlert bool) error {
	return c.Call(ctx, "answerCallbackQuery", map[string]any{
		"callback_query_id": id,
		"text":              text,
		"show_alert":        showAlert,
	}, nil)
}

// DeleteMessage deletes a message.
func (c *Client) DeleteMessage(ctx context.Context, chatID int64, messageID int) error {
	return c.Call(ctx, "deleteMessage", map[string]any{"chat_id": chatID, "message_id": messageID}, nil)
}

// SendDocument uploads a file as a document.
func (c *Client) SendDocument(ctx context.Context, chatID int64, fileName string, data []byte, caption string) (*Message, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	if caption != "" {
		_ = w.WriteField("caption", caption)
	}
	part, err := w.CreateFormFile("document", fileName)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	body := buf.Bytes()
	var m Message
	err = c.do(ctx, "sendDocument", w.FormDataContentType(), func() io.Reader { return bytes.NewReader(body) }, &m)
	return &m, err
}

// SetMyCommands sets the command menu.
func (c *Client) SetMyCommands(ctx context.Context, cmds []BotCommand) error {
	return c.Call(ctx, "setMyCommands", map[string]any{"commands": cmds}, nil)
}

// SetWebhook registers url as the webhook. secret (optional) is echoed by
// Telegram in the X-Telegram-Bot-Api-Secret-Token header.
func (c *Client) SetWebhook(ctx context.Context, url, secret string, allowedUpdates []string) error {
	p := map[string]any{"url": url, "allowed_updates": allowedUpdates}
	if secret != "" {
		p["secret_token"] = secret
	}
	return c.Call(ctx, "setWebhook", p, nil)
}

// DeleteWebhook removes the webhook so getUpdates can be used.
func (c *Client) DeleteWebhook(ctx context.Context) error {
	return c.Call(ctx, "deleteWebhook", struct{}{}, nil)
}

// GetUpdates long-polls for updates after offset, waiting up to timeout.
func (c *Client) GetUpdates(ctx context.Context, offset int, timeout time.Duration, allowedUpdates []string) ([]Update, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout+15*time.Second)
	defer cancel()
	var ups []Update
	err := c.Call(ctx, "getUpdates", map[string]any{
		"offset":          offset,
		"timeout":         int(timeout.Seconds()),
		"allowed_updates": allowedUpdates,
	}, &ups)
	return ups, err
}

// Poll long-polls until ctx is cancelled, calling handle for every update.
// handle should return quickly (start a goroutine for slow work).
func (c *Client) Poll(ctx context.Context, allowedUpdates []string, handle func(Update)) {
	offset := 0
	backoff := time.Second
	for ctx.Err() == nil {
		ups, err := c.GetUpdates(ctx, offset, 50*time.Second, allowedUpdates)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("getUpdates: %v (coba lagi dalam %s)", err, backoff)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return
			}
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
		for _, u := range ups {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			handle(u)
		}
	}
}
