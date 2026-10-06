package itchio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type PricingModel uint8

const (
	PricingFree PricingModel = iota
	PricingNameYourOwnPrice
	PricingPaid
)

// GameData is public metadata supplied by itch.io's per-game data.json.
// Monetary values retain the server's currency and formatting.
type GameData struct {
	ID             int64     `json:"id"`
	Title          string    `json:"title"`
	Price          string    `json:"price"`
	SuggestedPrice string    `json:"suggested_price"`
	OriginalPrice  string    `json:"original_price"`
	Sale           *GameSale `json:"sale"`
	Screenshots    []string  `json:"screenshots"`
	Tags           []string  `json:"tags"`
	CoverImage     string    `json:"cover_image"`
	URL            string    `json:"-"`
	Links          struct {
		Self string `json:"self"`
	} `json:"links"`
}

type GameSale struct {
	Title   string `json:"title"`
	Rate    int    `json:"rate"`
	EndDate string `json:"end_date"`
}

func (d *GameData) Pricing() PricingModel {
	if strings.TrimSpace(d.Price) == "" {
		return PricingFree
	}
	// Zero amounts may use another currency or a decimal comma. An
	// unrecognized price without digits must not enable a free download.
	hasDigit := false
	for _, r := range d.Price {
		if r >= '1' && r <= '9' {
			return PricingPaid
		}
		hasDigit = hasDigit || r == '0'
	}
	if hasDigit {
		return PricingNameYourOwnPrice
	}
	return PricingPaid
}

const dataJSONMaxBytes = 1 << 20

func (c *Client) FetchGameData(gameURL string) (*GameData, error) {
	return c.FetchGameDataContext(context.Background(), gameURL)
}

func (c *Client) FetchGameDataContext(ctx context.Context, gameURL string) (*GameData, error) {
	u, err := url.Parse(gameURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid game metadata address")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/data.json"
	u.RawPath, u.RawQuery, u.Fragment = "", "", ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, safeRequestError("build game metadata request", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, safeRequestError("fetch game metadata", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound, http.StatusGone:
		return nil, ErrGameRemoved
	case http.StatusTooManyRequests:
		return nil, ErrRateLimited
	case http.StatusOK:
	default:
		return nil, &metadataHTTPError{operation: "fetch game metadata", status: resp.StatusCode}
	}
	// Distinguish a failed read from a complete but malformed response. The
	// latter must never masquerade as a network outage or trigger reconnects.
	body, err := io.ReadAll(io.LimitReader(resp.Body, dataJSONMaxBytes+1))
	if err != nil {
		return nil, safeRequestError("read game metadata", err)
	}
	if len(body) > dataJSONMaxBytes {
		return nil, fmt.Errorf("game metadata exceeds the size limit")
	}
	var data GameData
	if err := json.Unmarshal(body, &data); err != nil || data.ID <= 0 {
		return nil, fmt.Errorf("invalid game metadata response")
	}
	c.rememberPrice(gameURL, data)
	data.URL = gameURL
	if canonical, err := url.Parse(data.Links.Self); err == nil && canonical.Host != "" && canonical.User == nil &&
		(canonical.Scheme == "https" || canonical.Scheme == "http") && canonical.RawQuery == "" && canonical.Fragment == "" {
		data.URL = canonical.String()
	}
	return &data, nil
}

func (c *Client) rememberPrice(gameURL string, data GameData) {
	c.pricesMu.Lock()
	defer c.pricesMu.Unlock()
	if c.prices == nil {
		c.prices = make(map[string]GameData)
	}
	c.prices[gameURL] = GameData{ID: data.ID, Price: data.Price, SuggestedPrice: data.SuggestedPrice,
		OriginalPrice: data.OriginalPrice}
}

// CachedPrice returns the price fields of the last data.json fetched for
// gameURL this session. They are current, unlike the catalogue feed's USD
// price, but only exist for games whose details or updates were loaded.
func (c *Client) CachedPrice(gameURL string) (GameData, bool) {
	c.pricesMu.Lock()
	defer c.pricesMu.Unlock()
	data, ok := c.prices[gameURL]
	return data, ok
}
