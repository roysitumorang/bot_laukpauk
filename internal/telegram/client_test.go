package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCommandParsing(t *testing.T) {
	cases := []struct {
		text, cmd, args string
		isCmd           bool
	}{
		{"/start", "start", "", true},
		{"/tutup_toko 01-01-2030 02-01-2030", "tutup_toko", "01-01-2030 02-01-2030", true},
		{"/set_harga@Laukpauk_Bot Brokoli Hijau 12000", "set_harga", "Brokoli Hijau 12000", true},
		{"brokoli", "", "", false},
	}
	for _, c := range cases {
		m := &Message{Text: c.text}
		if c.isCmd {
			tok := strings.Fields(c.text)[0]
			m.Entities = []MessageEntity{{Type: "bot_command", Offset: 0, Length: len(tok)}}
		}
		if m.IsCommand() != c.isCmd || m.Command() != c.cmd || m.CommandArguments() != c.args {
			t.Errorf("%q: IsCommand=%v Command=%q Args=%q", c.text, m.IsCommand(), m.Command(), m.CommandArguments())
		}
	}
}

func TestUpdateJSON(t *testing.T) {
	raw := `{"update_id":7,"callback_query":{"id":"q1","from":{"id":42,"is_bot":false,"first_name":"Budi"},
	  "message":{"message_id":9,"chat":{"id":42,"type":"private"},"date":1,"text":"hi"},"data":"pay_qris"}}`
	var u Update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	if u.CallbackQuery == nil || u.CallbackQuery.From.ID != 42 || u.CallbackQuery.Message.Chat.ID != 42 || u.CallbackQuery.Data != "pay_qris" {
		t.Fatalf("decoded %+v", u.CallbackQuery)
	}
}

func TestClientCalls(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/botT0K/sendMessage":
			var p SendMessageParams
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.ChatID != 5 || p.ReplyMarkup.InlineKeyboard[0][0].CallbackData != "x" {
				t.Errorf("bad sendMessage body: %+v %v", p, err)
			}
			io.WriteString(w, `{"ok":true,"result":{"message_id":3,"chat":{"id":5},"text":"halo"}}`)
		case "/botT0K/editMessageText":
			io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: message is not modified"}`)
		case "/botT0K/answerCallbackQuery":
			// Rate limited once, then succeeds.
			if hits.Load() == 3 {
				io.WriteString(w, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1}}`)
				return
			}
			io.WriteString(w, `{"ok":true,"result":true}`)
		case "/botT0K/sendDocument":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
			}
			f, h, err := r.FormFile("document")
			if err != nil || h.Filename != "a.pdf" || r.FormValue("chat_id") != "5" || r.FormValue("caption") != "cap" {
				t.Errorf("bad upload: %v", err)
			} else {
				b, _ := io.ReadAll(f)
				if string(b) != "%PDF" {
					t.Errorf("file content %q", b)
				}
			}
			io.WriteString(w, `{"ok":true,"result":{"message_id":4,"chat":{"id":5}}}`)
		default:
			io.WriteString(w, `{"ok":false,"error_code":404,"description":"Not Found"}`)
		}
	}))
	defer srv.Close()
	c := NewWithEndpoint("T0K", srv.URL)
	ctx := context.Background()

	m, err := c.SendMessage(ctx, SendMessageParams{ChatID: 5, Text: "halo",
		ReplyMarkup: &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{CallbackButton("A", "x")}}}})
	if err != nil || m.MessageID != 3 {
		t.Fatalf("SendMessage: %+v %v", m, err)
	}
	if err := c.EditMessageText(ctx, EditMessageTextParams{ChatID: 5, MessageID: 3, Text: "halo"}); !IsNotModified(err) {
		t.Fatalf("expected not-modified error, got %v", err)
	}
	start := time.Now()
	if err := c.AnswerCallbackQuery(ctx, "q", "", false); err != nil {
		t.Fatalf("AnswerCallbackQuery after retry: %v", err)
	}
	if time.Since(start) < time.Second {
		t.Error("429 retry did not wait retry_after")
	}
	if _, err := c.SendDocument(ctx, 5, "a.pdf", []byte("%PDF"), "cap"); err != nil {
		t.Fatal(err)
	}
	err = c.DeleteWebhook(ctx)
	var apiErr *Error
	if err == nil || !strings.Contains(err.Error(), "404") || !errorsAs(err, &apiErr) {
		t.Fatalf("expected API error, got %v", err)
	}
}

func errorsAs(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}

func TestTokenNotLeaked(t *testing.T) {
	c := NewWithEndpoint("SECRET123", "http://127.0.0.1:1") // nothing listens here
	_, err := c.GetMe(context.Background())
	if err == nil || strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("token leaked or no error: %v", err)
	}
}

func TestPoll(t *testing.T) {
	var offsets []float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		json.NewDecoder(r.Body).Decode(&p)
		offsets = append(offsets, p["offset"].(float64))
		if len(offsets) == 1 {
			io.WriteString(w, `{"ok":true,"result":[{"update_id":10,"message":{"message_id":1,"chat":{"id":1},"text":"a"}},{"update_id":11,"message":{"message_id":2,"chat":{"id":1},"text":"b"}}]}`)
			return
		}
		io.WriteString(w, `{"ok":true,"result":[]}`)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	var got []string
	NewWithEndpoint("T", srv.URL).Poll(ctx, nil, func(u Update) {
		got = append(got, u.Message.Text)
		if len(got) == 2 {
			cancel()
		}
	})
	if strings.Join(got, "") != "ab" || len(offsets) < 1 || offsets[0] != 0 {
		t.Fatalf("got %v offsets %v", got, offsets)
	}
}
