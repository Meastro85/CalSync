package icalhelper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/apognu/gocal"
)

const maxBody = 10 << 20

var client = &http.Client{Timeout: 30 * time.Second}

func GetIcal(ctx context.Context, feedURL string, from, to time.Time) ([]gocal.Event, error) {
	feedURL = strings.Replace(feedURL, "webcal://", "https://", 1)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, errors.New("icalhelper: invalid feed URL")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("icalhelper: request failed: %w", stripURL(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("icalhelper: unexpected status: %s", resp.Status)
	}

	c := gocal.NewParser(io.LimitReader(resp.Body, maxBody))
	c.Start, c.End = &from, &to

	if err := c.Parse(); err != nil {
		return nil, fmt.Errorf("icalhelper: parse: %w", err)
	}
	return c.Events, nil
}

func stripURL(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return ue.Err
	}
	return err
}
