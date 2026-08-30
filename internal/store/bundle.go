package store

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidBundleName       = errors.New("bundle name is required")
	ErrInvalidBundlePrice      = errors.New("bundle price must be greater than zero")
	ErrInvalidRegularPrice     = errors.New("regular price must be at least the bundle price")
	ErrInvalidBundleMembership = errors.New("bundle needs at least two distinct items")
)

// Bundle is one store package considered across multiple items.
type Bundle struct {
	ID                int64
	Name              string
	URL               string
	PriceCents        int64
	RegularPriceCents *int64
	CreatedAt         time.Time
	Members           []BundleMember
}

// BundleInput is the user-supplied form of a bundle.
type BundleInput struct {
	Name         string
	URL          string
	Price        string
	RegularPrice string
	Members      []BundleMemberInput
}

// BundleMemberInput identifies one Item and its optional store-facing label.
type BundleMemberInput struct {
	ItemID         int64
	ComponentLabel string
}

type cleanBundle struct {
	name              string
	url               string
	priceCents        int64
	regularPriceCents *int64
	members           []BundleMemberInput
}

var moneyInput = regexp.MustCompile(`^[0-9]+(?:\.[0-9]{1,2})?$`)

func (in BundleInput) clean() (cleanBundle, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return cleanBundle{}, ErrInvalidBundleName
	}

	rawURL := strings.TrimSpace(in.URL)
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return cleanBundle{}, ErrInvalidURL
	}

	price, err := parseBundlePrice(in.Price, false)
	if err != nil {
		return cleanBundle{}, err
	}
	regular, err := parseBundlePrice(in.RegularPrice, true)
	if err != nil {
		return cleanBundle{}, err
	}
	if regular != nil && *regular < *price {
		return cleanBundle{}, ErrInvalidRegularPrice
	}

	if len(in.Members) < 2 {
		return cleanBundle{}, ErrInvalidBundleMembership
	}
	members := make([]BundleMemberInput, len(in.Members))
	seen := make(map[int64]struct{}, len(in.Members))
	for i, member := range in.Members {
		if member.ItemID <= 0 {
			return cleanBundle{}, ErrInvalidBundleMembership
		}
		if _, ok := seen[member.ItemID]; ok {
			return cleanBundle{}, ErrInvalidBundleMembership
		}
		seen[member.ItemID] = struct{}{}
		member.ComponentLabel = strings.TrimSpace(member.ComponentLabel)
		members[i] = member
	}

	return cleanBundle{name: name, url: u.String(), priceCents: *price, regularPriceCents: regular, members: members}, nil
}

func parseBundlePrice(raw string, optional bool) (*int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" && optional {
		return nil, nil
	}
	if raw == "" || !moneyInput.MatchString(raw) {
		return nil, ErrInvalidBundlePrice
	}
	parts := strings.SplitN(raw, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > (1<<63-1)/100 {
		return nil, ErrInvalidBundlePrice
	}
	cents := whole * 100
	if len(parts) == 2 {
		fraction := parts[1] + "0"
		value, err := strconv.ParseInt(fraction[:2], 10, 64)
		if err != nil {
			return nil, ErrInvalidBundlePrice
		}
		cents += value
	}
	if cents <= 0 {
		return nil, ErrInvalidBundlePrice
	}
	return &cents, nil
}

// BundleMember connects a bundle to one item and its generated option.
type BundleMember struct {
	ID             int64
	BundleID       int64
	ItemID         int64
	OptionID       int64
	Position       int
	ComponentLabel string
}

// BundleComment is one shared remark about a bundle.
type BundleComment struct {
	ID        int64
	BundleID  int64
	Body      string
	CreatedAt time.Time
}
