// Package state keeps per-user conversation state, vouchers and pending
// orders, persisted to a JSON file so a restart does not lose carts or
// unpaid invoices.
package state

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/roysitumorang/bot_laukpauk/internal/order"
)

// Checkout steps.
const (
	StepNama            = "nama"
	StepAlamat          = "alamat"
	StepDetailAlamat    = "detail_alamat"
	StepHP              = "hp"
	StepVoucher         = "voucher"
	StepPoin            = "poin"
	StepWaktuPengiriman = "waktu_pengiriman"
	StepMetodeBayar     = "metode_pembayaran"
	StepInvoiceDikirim  = "invoice_dikirim"
	StepDone            = "done"
)

// Rating steps.
const (
	RateRating    = "rating"
	RateTestimoni = "testimoni"
)

// Session is everything the bot remembers about one Telegram user between
// messages (the equivalent of context.user_data plus the cart).
type Session struct {
	// Customer data collected at checkout.
	Nama         string  `json:"nama,omitempty"`
	Alamat       string  `json:"alamat,omitempty"`
	AlamatRaw    string  `json:"alamat_raw,omitempty"`
	DetailAlamat string  `json:"detail_alamat,omitempty"`
	HP           string  `json:"hp,omitempty"`
	JarakKm      float64 `json:"jarak_km,omitempty"`
	Ongkir       int64   `json:"ongkir,omitempty"`
	TotalPoin    int64   `json:"total_poin,omitempty"` // balance shown at checkout

	// Current order.
	Cart            order.Cart `json:"cart,omitempty"`
	CheckoutStep    string     `json:"checkout_step,omitempty"`
	Voucher         string     `json:"voucher,omitempty"`
	Poin            int64      `json:"poin,omitempty"` // points the customer asked to redeem
	WaktuPengiriman string     `json:"waktu_pengiriman,omitempty"`
	InvoiceID       string     `json:"invoice_id,omitempty"`
	InvoiceURL      string     `json:"invoice_url,omitempty"`

	// Rating / testimonial conversation.
	RateStep string `json:"rate_step,omitempty"`
	Rating   int    `json:"rating,omitempty"`
}

// Clone returns a deep copy.
func (s Session) Clone() Session {
	s.Cart = s.Cart.Clone()
	return s
}

// ResetOrder clears everything about the current order, including the cart.
func (s *Session) ResetOrder() {
	s.Cart = nil
	s.ResetCheckout()
}

// ResetCheckout clears the checkout progress but keeps the cart and the
// customer's contact data.
func (s *Session) ResetCheckout() {
	s.CheckoutStep = ""
	s.Voucher = ""
	s.Poin = 0
	s.WaktuPengiriman = ""
	s.InvoiceID = ""
	s.InvoiceURL = ""
}

// PendingOrder is a snapshot of an order taken when its invoice is created.
// The Xendit webhook completes the order from this snapshot, so later
// changes to the user's cart cannot alter what was paid for.
type PendingOrder struct {
	InvoiceID       string       `json:"invoice_id"`
	ExternalID      string       `json:"external_id"`
	InvoiceURL      string       `json:"invoice_url"`
	UserID          int64        `json:"user_id"`
	Nama            string       `json:"nama"`
	Alamat          string       `json:"alamat"`
	DetailAlamat    string       `json:"detail_alamat"`
	HP              string       `json:"hp"`
	WaktuPengiriman string       `json:"waktu_pengiriman"`
	Voucher         string       `json:"voucher,omitempty"`
	JarakKm         float64      `json:"jarak_km"`
	Cart            order.Cart   `json:"cart"`
	Totals          order.Totals `json:"totals"`
	CreatedAt       time.Time    `json:"created_at"`
}

type fileData struct {
	Sessions  map[int64]Session        `json:"sessions"`
	Vouchers  map[string]order.Voucher `json:"vouchers"`
	Pending   map[string]*PendingOrder `json:"pending"`
	Processed map[string]time.Time     `json:"processed"` // invoice IDs already completed
}

// Store is safe for concurrent use.
type Store struct {
	path string

	mu   sync.Mutex
	data fileData

	locks sync.Map // int64 -> *sync.Mutex
}

// Open loads (or creates) dir/state.json.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "state.json")}
	b, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &s.data); err != nil {
			return nil, fmt.Errorf("parse %s: %w", s.path, err)
		}
	case os.IsNotExist(err):
	default:
		return nil, err
	}
	if s.data.Sessions == nil {
		s.data.Sessions = map[int64]Session{}
	}
	if s.data.Vouchers == nil {
		s.data.Vouchers = order.DefaultVouchers()
	}
	if s.data.Pending == nil {
		s.data.Pending = map[string]*PendingOrder{}
	}
	if s.data.Processed == nil {
		s.data.Processed = map[string]time.Time{}
	}
	return s, nil
}

// LockUser serialises work for one user; call the returned func to unlock.
func (s *Store) LockUser(userID int64) func() {
	m, _ := s.locks.LoadOrStore(userID, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// saveLocked writes the file atomically. s.mu must be held.
func (s *Store) saveLocked() {
	// Forget completed invoices after 30 days.
	cutoff := time.Now().AddDate(0, 0, -30)
	for id, t := range s.data.Processed {
		if t.Before(cutoff) {
			delete(s.data.Processed, id)
		}
	}
	b, err := json.Marshal(s.data)
	if err != nil {
		log.Printf("state: marshal: %v", err)
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Printf("state: write: %v", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		log.Printf("state: rename: %v", err)
	}
}

// Get returns a copy of the user's session.
func (s *Store) Get(userID int64) Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Sessions[userID].Clone()
}

// Put stores the session (persisting only when something changed).
func (s *Store) Put(userID int64, sess Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before, _ := json.Marshal(s.data.Sessions[userID])
	after, _ := json.Marshal(sess)
	if string(before) == string(after) {
		return
	}
	s.data.Sessions[userID] = sess.Clone()
	s.saveLocked()
}

// Update applies fn to the user's session under the user lock. Do not call
// it for a user whose lock the caller already holds.
func (s *Store) Update(userID int64, fn func(*Session)) {
	unlock := s.LockUser(userID)
	defer unlock()
	sess := s.Get(userID)
	fn(&sess)
	s.Put(userID, sess)
}

// Vouchers returns a copy of all vouchers.
func (s *Store) Vouchers() map[string]order.Voucher {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]order.Voucher, len(s.data.Vouchers))
	for k, v := range s.data.Vouchers {
		out[k] = v
	}
	return out
}

// Voucher looks one voucher up.
func (s *Store) Voucher(kode string) (order.Voucher, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data.Vouchers[kode]
	return v, ok
}

// PutVoucher adds or replaces a voucher.
func (s *Store) PutVoucher(v order.Voucher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Vouchers[v.Kode] = v
	s.saveLocked()
}

// AddPending records an order awaiting payment.
func (s *Store) AddPending(p *PendingOrder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *p
	cp.Cart = p.Cart.Clone()
	s.data.Pending[p.InvoiceID] = &cp
	s.saveLocked()
}

// Pending returns a copy of a pending order.
func (s *Store) Pending(invoiceID string) (*PendingOrder, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.data.Pending[invoiceID]
	if !ok {
		return nil, false
	}
	cp := *p
	cp.Cart = p.Cart.Clone()
	return &cp, true
}

// DropPending forgets a pending order (cancelled or expired).
func (s *Store) DropPending(invoiceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data.Pending[invoiceID]; ok {
		delete(s.data.Pending, invoiceID)
		s.saveLocked()
	}
}

// ClaimPending atomically moves a pending order to the processed set. It
// returns false if the invoice is unknown or was already processed, which
// makes webhook retries harmless.
func (s *Store) ClaimPending(invoiceID string) (*PendingOrder, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, done := s.data.Processed[invoiceID]; done {
		return nil, false
	}
	p, ok := s.data.Pending[invoiceID]
	if !ok {
		return nil, false
	}
	delete(s.data.Pending, invoiceID)
	s.data.Processed[invoiceID] = time.Now()
	s.saveLocked()
	return p, true
}

// MarkProcessed records an invoice as completed without a pending entry
// (used for orders fully paid with points).
func (s *Store) MarkProcessed(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Processed[id] = time.Now()
	s.saveLocked()
}
