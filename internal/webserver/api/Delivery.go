package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/delivery"
	"github.com/Activer007/SupaShare/internal/webserver/errorHandling/errorcodes"
)

type deliveryOutput struct {
	Token     string   `json:"token"`
	Title     string   `json:"title"`
	Note      string   `json:"note"`
	FileIDs   []string `json:"fileIds"`
	CreatedAt int64    `json:"createdAt"`
	ExpiresAt int64    `json:"expiresAt"`
	RevokedAt int64    `json:"revokedAt"`
	URL       string   `json:"url"`
}

func outputDelivery(d models.Delivery) deliveryOutput {
	return deliveryOutput{Token: d.Token, Title: d.Title, Note: d.Note, FileIDs: d.FileIDs,
		CreatedAt: d.CreatedAt, ExpiresAt: d.ExpiresAt, RevokedAt: d.RevokedAt, URL: configuration.Get().ServerUrl + "delivery/" + d.Token}
}

func decodeDeliveryBody(r *http.Request, target any) error {
	if r.Method != http.MethodPost {
		return errors.New("POST required")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, (16<<10)+1))
	if err != nil || len(body) > 16<<10 {
		return errors.New("invalid delivery request")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid delivery request")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("invalid delivery request")
	}
	return nil
}

func (p *paramDeliveryCreate) ProcessParameter(r *http.Request) error {
	p.Request = r
	return decodeDeliveryBody(r, &p.Input)
}

func (p *paramDeliveryRevoke) ProcessParameter(r *http.Request) error {
	p.Request = r
	var input struct {
		Token string `json:"token"`
	}
	if err := decodeDeliveryBody(r, &input); err != nil {
		return err
	}
	if !delivery.ValidToken(input.Token) {
		return errors.New("invalid delivery request")
	}
	p.Token = input.Token
	return nil
}

func (p *paramDeliveryList) ProcessParameter(r *http.Request) error {
	if r.Method != http.MethodGet {
		return errors.New("GET required")
	}
	p.Request = r
	return nil
}

func apiDeliveryCreate(w http.ResponseWriter, parsed requestParser, user models.User, _ models.ApiKey) {
	p := parsed.(*paramDeliveryCreate)
	d, err := delivery.Create(p.Request.Context(), user.Id, p.Input)
	if err != nil {
		status, code, message := http.StatusServiceUnavailable, errorcodes.InvalidUserInput, "Delivery temporarily unavailable"
		if errors.Is(err, delivery.ErrInvalid) {
			status, message = http.StatusBadRequest, "Invalid delivery input"
		}
		if errors.Is(err, delivery.ErrFileUnavailable) {
			status, message = http.StatusBadRequest, "Select available ordinary shares that you own; E2E files are excluded"
		}
		sendError(w, status, code, message)
		return
	}
	_ = json.NewEncoder(w).Encode(outputDelivery(d))
}

func apiDeliveryList(w http.ResponseWriter, parsed requestParser, user models.User, _ models.ApiKey) {
	p := parsed.(*paramDeliveryList)
	list, err := database.ListDeliveries(p.Request.Context(), user.Id)
	if err != nil {
		sendError(w, http.StatusServiceUnavailable, errorcodes.InvalidUserInput, "Delivery temporarily unavailable")
		return
	}
	output := make([]deliveryOutput, 0, len(list))
	for _, d := range list {
		if d.OwnerID != user.Id {
			continue
		}
		output = append(output, outputDelivery(d))
	}
	_ = json.NewEncoder(w).Encode(output)
}

func apiDeliveryRevoke(w http.ResponseWriter, parsed requestParser, user models.User, _ models.ApiKey) {
	p := parsed.(*paramDeliveryRevoke)
	found, err := database.RevokeDelivery(p.Request.Context(), p.Token, user.Id, time.Now().Unix())
	if err != nil {
		sendError(w, http.StatusServiceUnavailable, errorcodes.InvalidUserInput, "Delivery temporarily unavailable")
		return
	}
	if !found {
		sendError(w, http.StatusNotFound, errorcodes.NotFound, "Delivery unavailable")
		return
	}
	_ = json.NewEncoder(w).Encode(struct{ Result string }{"OK"})
}
