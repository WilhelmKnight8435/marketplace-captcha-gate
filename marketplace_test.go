package marketgate

import (
	"context"
	"errors"
	"testing"
)

type fakeVerifier struct {
	result  CaptchaResult
	err     error
	calls   int
	request CaptchaRequest
}

func (f *fakeVerifier) Verify(_ context.Context, request CaptchaRequest) (CaptchaResult, error) {
	f.calls++
	f.request = request
	return f.result, f.err
}

func TestSignupSendsWidgetRecordID(t *testing.T) {
	verifier := &fakeVerifier{result: CaptchaResult{Valid: true, Action: "marketplace_signup"}}
	market := NewMarketplace(verifier)

	if _, err := market.Signup(context.Background(), validSignup("signup-widget")); err != nil {
		t.Fatal(err)
	}
	if verifier.request.WidgetRecordID != "widget-record-1" {
		t.Fatalf("widget_record_id = %q, want widget-record-1", verifier.request.WidgetRecordID)
	}
}

func TestSignupDecision(t *testing.T) {
	tests := []struct {
		name       string
		verifier   *fakeVerifier
		wantErr    error
		wantSeller bool
	}{
		{name: "accepted captcha creates seller asset", verifier: &fakeVerifier{result: CaptchaResult{Valid: true, Action: "marketplace_signup"}}, wantSeller: true},
		{name: "invalid captcha blocks registration", verifier: &fakeVerifier{result: CaptchaResult{Valid: false}}, wantErr: ErrCaptchaRejected},
		{name: "business rejection blocks registration", verifier: &fakeVerifier{err: &InfraiError{HTTPStatus: 422, Message: "score below threshold"}}, wantErr: ErrCaptchaRejected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			market := NewMarketplace(tt.verifier)
			seller, err := market.Signup(context.Background(), validSignup("signup-1"))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Signup() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantSeller && (seller.ID == "" || len(seller.Assets) != 1) {
				t.Fatalf("Signup() seller = %#v, want one asset", seller)
			}
		})
	}
}

func TestSignupReplayAndOrderHandoff(t *testing.T) {
	verifier := &fakeVerifier{result: CaptchaResult{Valid: true, Action: "marketplace_signup"}}
	market := NewMarketplace(verifier)

	first, err := market.Signup(context.Background(), validSignup("signup-42"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := market.Signup(context.Background(), validSignup("signup-42"))
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || verifier.calls != 1 {
		t.Fatalf("replay created a second seller: first=%s second=%s calls=%d", first.ID, second.ID, verifier.calls)
	}

	handoff, err := market.CreateOrder(OrderInput{BuyerID: "buyer_7", AssetID: first.Assets[0].ID, IdempotencyKey: "order-7"})
	if err != nil {
		t.Fatal(err)
	}
	if handoff.State != "ready_for_buyer" || handoff.BuyerUpdate.Status != "asset_ready" {
		t.Fatalf("handoff = %#v", handoff)
	}
}

func validSignup(key string) SignupInput {
	return SignupInput{
		Email: "seller@example.com", Password: "correct-horse-battery-staple", Name: "Ada",
		CaptchaWidgetRecordID: "widget-record-1", CaptchaToken: "browser-token", CaptchaVendor: "turnstile", AssetTitle: "Invoice templates",
		AssetDownload: "catalog/invoice-templates", IdempotencyKey: key,
	}
}
