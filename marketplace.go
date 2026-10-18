package marketgate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
)

var (
	ErrCaptchaRejected = errors.New("captcha rejected")
	ErrInvalidSignup   = errors.New("email, password, captcha_widget_record_id, captcha_token, and asset are required")
	ErrInvalidOrder    = errors.New("buyer_id and asset_id are required")
	ErrAssetNotFound   = errors.New("asset not found")
)

type CaptchaVerifier interface {
	Verify(context.Context, CaptchaRequest) (CaptchaResult, error)
}

type SellerAsset struct {
	ID          string `json:"id"`
	SellerID    string `json:"seller_id"`
	Title       string `json:"title"`
	DownloadRef string `json:"download_ref"`
}

type Seller struct {
	ID     string        `json:"id"`
	Email  string        `json:"email"`
	Name   string        `json:"name"`
	Assets []SellerAsset `json:"assets"`
}

type SignupInput struct {
	Email                 string `json:"email"`
	Password              string `json:"password"`
	Name                  string `json:"name"`
	CaptchaWidgetRecordID string `json:"captcha_widget_record_id"`
	CaptchaToken          string `json:"captcha_token"`
	CaptchaVendor         string `json:"captcha_vendor"`
	IP                    string `json:"-"`
	AssetTitle            string `json:"asset_title"`
	AssetDownload         string `json:"asset_download"`
	IdempotencyKey        string `json:"-"`
}

type BuyerUpdate struct {
	BuyerID string `json:"buyer_id"`
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

type OrderHandoff struct {
	OrderID     string      `json:"order_id"`
	Asset       SellerAsset `json:"asset"`
	BuyerUpdate BuyerUpdate `json:"buyer_update"`
	State       string      `json:"state"`
}

type OrderInput struct {
	BuyerID        string `json:"buyer_id"`
	AssetID        string `json:"asset_id"`
	IdempotencyKey string `json:"-"`
}

type Marketplace struct {
	verifier CaptchaVerifier
	mu       sync.Mutex
	sequence int
	signups  map[string]Seller
	orders   map[string]OrderHandoff
	assets   map[string]SellerAsset
}

func NewMarketplace(verifier CaptchaVerifier) *Marketplace {
	return &Marketplace{
		verifier: verifier,
		signups:  make(map[string]Seller),
		orders:   make(map[string]OrderHandoff),
		assets:   make(map[string]SellerAsset),
	}
}

func (m *Marketplace) Signup(ctx context.Context, input SignupInput) (Seller, error) {
	if input.Email == "" || input.Password == "" || input.CaptchaWidgetRecordID == "" || input.CaptchaToken == "" || input.AssetTitle == "" || input.AssetDownload == "" || input.IdempotencyKey == "" {
		return Seller{}, ErrInvalidSignup
	}

	m.mu.Lock()
	if seller, ok := m.signups[input.IdempotencyKey]; ok {
		m.mu.Unlock()
		return seller, nil
	}
	m.mu.Unlock()

	result, err := m.verifier.Verify(ctx, CaptchaRequest{
		WidgetRecordID: input.CaptchaWidgetRecordID,
		Token:          input.CaptchaToken,
		Vendor:         input.CaptchaVendor,
		IP:             input.IP,
		Action:         "marketplace_signup",
		ScoreThreshold: 0.7,
	})
	if err != nil {
		var apiErr *InfraiError
		if errors.As(err, &apiErr) && (apiErr.HTTPStatus == http.StatusBadRequest || apiErr.HTTPStatus == http.StatusUnprocessableEntity) {
			return Seller{}, fmt.Errorf("%w: %s", ErrCaptchaRejected, apiErr.Message)
		}
		return Seller{}, err
	}
	if !result.Valid || (result.Action != "" && result.Action != "marketplace_signup") {
		return Seller{}, ErrCaptchaRejected
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if seller, ok := m.signups[input.IdempotencyKey]; ok {
		return seller, nil
	}
	m.sequence++
	sellerID := fmt.Sprintf("seller_%d", m.sequence)
	m.sequence++
	asset := SellerAsset{ID: fmt.Sprintf("asset_%d", m.sequence), SellerID: sellerID, Title: input.AssetTitle, DownloadRef: input.AssetDownload}
	seller := Seller{ID: sellerID, Email: input.Email, Name: input.Name, Assets: []SellerAsset{asset}}
	m.signups[input.IdempotencyKey] = seller
	m.assets[asset.ID] = asset
	return seller, nil
}

func (m *Marketplace) CreateOrder(input OrderInput) (OrderHandoff, error) {
	if input.BuyerID == "" || input.AssetID == "" || input.IdempotencyKey == "" {
		return OrderHandoff{}, ErrInvalidOrder
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if handoff, ok := m.orders[input.IdempotencyKey]; ok {
		return handoff, nil
	}
	asset, ok := m.assets[input.AssetID]
	if !ok {
		return OrderHandoff{}, ErrAssetNotFound
	}
	m.sequence++
	orderID := fmt.Sprintf("order_%d", m.sequence)
	handoff := OrderHandoff{
		OrderID:     orderID,
		Asset:       asset,
		BuyerUpdate: BuyerUpdate{BuyerID: input.BuyerID, OrderID: orderID, Status: "asset_ready"},
		State:       "ready_for_buyer",
	}
	m.orders[input.IdempotencyKey] = handoff
	return handoff, nil
}
