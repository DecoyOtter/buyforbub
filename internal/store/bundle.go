package store

import "time"

// Bundle is one store package considered across multiple items.
type Bundle struct {
	ID                int64
	Name              string
	URL               string
	PriceCents        int64
	RegularPriceCents *int64
	CreatedAt         time.Time
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
