package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	marketgate "example.com/marketplace-captcha-gate"
)

type server struct {
	market *marketgate.Marketplace
}

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	s := &server{market: marketgate.NewMarketplace(marketgate.NewCaptchaClient(apiKey))}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /marketplace/signup", s.signup)
	mux.HandleFunc("POST /marketplace/orders", s.createOrder)
	log.Println("marketplace listening on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func (s *server) signup(w http.ResponseWriter, r *http.Request) {
	var input marketgate.SignupInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	input.IdempotencyKey = r.Header.Get("Idempotency-Key")
	input.IP = clientIP(r)
	seller, err := s.market.Signup(r.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, marketgate.ErrCaptchaRejected):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "captcha rejected"})
		case errors.Is(err, marketgate.ErrInvalidSignup):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "captcha service request failed"})
		}
		return
	}
	writeJSON(w, http.StatusCreated, seller)
}

func (s *server) createOrder(w http.ResponseWriter, r *http.Request) {
	var input marketgate.OrderInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	input.IdempotencyKey = r.Header.Get("Idempotency-Key")
	handoff, err := s.market.CreateOrder(input)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, marketgate.ErrAssetNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, handoff)
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
