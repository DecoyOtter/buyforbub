package store

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidURL   = errors.New("not a usable link")
	ErrInvalidPrice = errors.New("price is not a number")
)

// Option is one candidate product for an item — normally a link to a shop.
type Option struct {
	ID         int64
	ItemID     int64
	URL        string
	Label      string
	PriceCents *int64
	Chosen     bool
	CreatedAt  time.Time
}

// Title is what to show for the option: its label, or the site it points at.
func (o Option) Title() string {
	if o.Label != "" {
		return o.Label
	}
	if u, err := url.Parse(o.URL); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Host, "www.")
	}
	return o.URL
}

// PriceText formats the price for display, or returns "" when there is none.
func (o Option) PriceText() string {
	if o.PriceCents == nil {
		return ""
	}
	whole, cents := *o.PriceCents/100, *o.PriceCents%100
	if cents == 0 {
		return "$" + addThousands(strconv.FormatInt(whole, 10))
	}
	return fmt.Sprintf("$%s.%02d", addThousands(strconv.FormatInt(whole, 10)), cents)
}

func addThousands(s string) string {
	if len(s) <= 3 {
		return s
	}
	return addThousands(s[:len(s)-3]) + "," + s[len(s)-3:]
}

// OptionInput is the user-supplied form of an option. Price is free text so a
// pasted "$1,199.00" is accepted as readily as "1199".
type OptionInput struct {
	URL   string
	Label string
	Price string
}

type cleanOption struct {
	url        string
	label      string
	priceCents *int64
}

func (in OptionInput) clean() (cleanOption, error) {
	raw := strings.TrimSpace(in.URL)
	if raw == "" {
		return cleanOption{}, ErrInvalidURL
	}
	// Accept a bare "shop.example.com/thing" as https.
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return cleanOption{}, ErrInvalidURL
	}

	price, err := parsePrice(in.Price)
	if err != nil {
		return cleanOption{}, err
	}

	return cleanOption{
		url:        u.String(),
		label:      strings.TrimSpace(in.Label),
		priceCents: price,
	}, nil
}

// parsePrice turns user text into cents. Blank means no price.
func parsePrice(s string) (*int64, error) {
	s = strings.TrimSpace(s)
	s = strings.NewReplacer("$", "", ",", "", " ", "").Replace(s)
	if s == "" {
		return nil, nil
	}

	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > float64(math.MaxInt64)/100 {
		return nil, ErrInvalidPrice
	}
	cents := int64(f*100 + 0.5)
	return &cents, nil
}
