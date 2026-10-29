// Package assets embeds static files used by the bot.
package assets

import _ "embed"

// Logo is the Laukpauk logo printed on PDF invoices.
//
//go:embed logoLP.jpg
var Logo []byte
