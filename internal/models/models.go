package models

import "time"

type ModelType string

const (
	ModelFlux2      ModelType = "flux-2"
	ModelNanoBanana ModelType = "nano-banana-pro"
)

type CostType string

const (
	CostTypeFree  CostType = "free"
	CostTypePromo CostType = "promo"
	CostTypePaid  CostType = "paid"
)

type User struct {
	ID             int64
	TelegramID     int64
	Username       string
	FirstName      string
	LastName       string
	FreeDailyLimit int
	PromoCredits   int
	PaidCredits    int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type GenerationLog struct {
	ID        int64
	UserID    int64
	Model     ModelType
	Prompt    string
	CostType  CostType
	CreatedAt time.Time
}

type PromoCode struct {
	ID      int64
	Code    string
	MaxUses int
	Uses    int
}

type Payment struct {
	ID             int64
	UserID         int64
	Provider       string
	ProviderCharge string
	Currency       string
	Amount         int
	Status         string
	RawPayload     string
	CreatedAt      time.Time
}
