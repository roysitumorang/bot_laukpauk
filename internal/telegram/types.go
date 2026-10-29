// Package telegram is a minimal Telegram Bot API client built only on the
// standard library. It covers exactly what the bot uses: receiving updates
// (webhook or long polling), sending/editing messages with inline keyboards,
// answering callback queries, sending documents and registering commands.
package telegram

import "strings"

// Update is an incoming update. Only messages and callback queries are used.
type Update struct {
	UpdateID      int            `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

// User is a Telegram user or bot.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

// Chat is the conversation a message belongs to.
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type,omitempty"`
}

// MessageEntity marks a special part of a message's text (e.g. a command).
type MessageEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

// Message is a chat message.
type Message struct {
	MessageID int             `json:"message_id"`
	From      *User           `json:"from,omitempty"`
	Chat      Chat            `json:"chat"`
	Date      int64           `json:"date,omitempty"`
	Text      string          `json:"text,omitempty"`
	Entities  []MessageEntity `json:"entities,omitempty"`
}

// IsCommand reports whether the message starts with a bot command.
func (m *Message) IsCommand() bool {
	if m == nil || len(m.Entities) == 0 {
		return false
	}
	e := m.Entities[0]
	return e.Offset == 0 && e.Type == "bot_command"
}

// commandToken returns the leading "/command@bot" token, if any.
func (m *Message) commandToken() string {
	if !m.IsCommand() {
		return ""
	}
	// Commands are ASCII, so the UTF-16 length equals the byte length.
	n := min(m.Entities[0].Length, len(m.Text))
	return m.Text[:n]
}

// Command returns the command without the leading slash and "@botname",
// e.g. "start" for "/start@Laukpauk_Bot".
func (m *Message) Command() string {
	cmd := strings.TrimPrefix(m.commandToken(), "/")
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	return cmd
}

// CommandArguments returns the text after the command.
func (m *Message) CommandArguments() string {
	tok := m.commandToken()
	if tok == "" {
		return ""
	}
	return strings.TrimSpace(m.Text[len(tok):])
}

// CallbackQuery is sent when an inline keyboard button is pressed.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from,omitempty"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data,omitempty"`
}

// InlineKeyboardButton is a button under a message. Set exactly one of
// CallbackData or URL.
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}

// InlineKeyboardMarkup is a grid of inline buttons.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// BotCommand is an entry of the bot's command menu.
type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// CallbackButton returns a button that sends data back to the bot.
func CallbackButton(text, data string) InlineKeyboardButton {
	return InlineKeyboardButton{Text: text, CallbackData: data}
}

// URLButton returns a button that opens a URL.
func URLButton(text, url string) InlineKeyboardButton {
	return InlineKeyboardButton{Text: text, URL: url}
}
