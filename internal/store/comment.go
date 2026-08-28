package store

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidComment = errors.New("comment is empty")

// Comment is one remark about an option — why it appeals, or why it does not.
type Comment struct {
	ID        int64
	OptionID  int64
	Body      string
	CreatedAt time.Time
}

// Age is how long ago the comment was left, at a glance: "now", "3h", "2d".
func (c Comment) Age() string { return ageSince(time.Now(), c.CreatedAt) }

func ageSince(now, then time.Time) string {
	d := now.Sub(then)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	case d < 7*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	default:
		return strconv.Itoa(int(d.Hours()/(24*7))) + "w"
	}
}

func cleanComment(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", ErrInvalidComment
	}
	return body, nil
}
