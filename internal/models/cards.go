package models

import "time"

// Card-related API and storage models

type Pagination struct {
	PageNumber uint `json:"pageNumber"`
	PageSize   uint `json:"pageSize"`
	TotalPages uint `json:"totalPages"`
}

type ListCardsResponse struct {
	Data       []Card     `json:"data"`
	Pagination Pagination `json:"pagination"`
}

type Customer struct {
	ID        *string                   `json:"id"`
	SourceID  string                    `json:"sourceId"`
	Type      string                    `json:"type"`
	Code      string                    `json:"code"`
	TaxNumber string                    `json:"taxNumber"`
	KYCStatus string                    `json:"kycStatus"`
	Addresses []CustomerDeliveryAddress `json:"addresses"`
	Accounts  []Account                 `json:"accounts"`
	CreatedAt time.Time                 `json:"createdAt"`
}

type CustomerResponse struct {
	WalletAddress string   `json:"walletAddress"`
	Customer      Customer `json:"customers"`
}

type Account struct {
	ID               *string   `json:"id"`
	SourceID         string    `json:"sourceId"`
	CustomerID       *string   `json:"customerId"`
	CustomerSourceID string    `json:"customerSourceId"`
	ProductCode      string    `json:"productCode"`
	Currency         string    `json:"currency"`
	AccountNumber    string    `json:"accountNumber"`
	Type             string    `json:"type"`
	Status           string    `json:"status"`
	StatusReasonCode string    `json:"statusReasonCode"`
	Cards            []Card    `json:"cards"`
	CreatedAt        time.Time `json:"createdAt"`
}

type Card struct {
	ID                         string    `json:"id"`
	SourceID                   string    `json:"sourceId"`
	AccountID                  string    `json:"accountId"`
	AccountSourceID            string    `json:"accountSourceId"`
	CustomerID                 string    `json:"customerId"`
	CustomerSourceID           string    `json:"customerSourceId"`
	NameOnCard                 string    `json:"nameOnCard"`
	ProductCode                string    `json:"productCode"`
	PanToken                   string    `json:"panToken"`
	MaskedPan                  string    `json:"maskedPan"`
	Status                     string    `json:"status"`
	StatusReasonCode           *string   `json:"statusReasonCode"`
	LockLevel                  *string   `json:"lockLevel"`
	ExpiryDate                 string    `json:"expiryDate"`
	RelationType               string    `json:"relationType"`
	MembershipFeeEffectiveDate *string   `json:"membershipFeeEffectiveDate"`
	IsFirstTimeLock            bool      `json:"isFirstTimeLock"`
	PlasticCreated             bool      `json:"plasticCreated"`
	OrderPlasticOnSync         bool      `json:"OrderPlasticOnSync"`
	CreatedAt                  time.Time `json:"createdAt"`
}

type CustomerDeliveryAddress struct {
	ID               string  `json:"id"`
	SourceID         string  `json:"sourceId"`
	CustomerID       string  `json:"customerId"`
	CustomerSourceID string  `json:"customerSourceId"`
	Type             string  `json:"type"`
	Line1            string  `json:"line1"`
	Line2            *string `json:"line2"`
	Line3            *string `json:"line3"`
	City             string  `json:"city"`
	PostOffice       *string `json:"postOffice"`
	ZipCode          string  `json:"zipCode"`
	CountryCode      string  `json:"countryCode"`
	Status           string  `json:"status"`
}

type NewCardArgs struct {
	ProductCode string `json:"productCode"`
}

type CardAccount struct {
	ProductCode string      `json:"productCode"`
	Currency    string      `json:"currency"`
	Card        NewCardArgs `json:"card"`
}

type CreateCustomerAndCardArgs struct {
	WalletAddress string                             `json:"walletAddress"`
	Account       CardAccount                        `json:"account"`
	Delivery      *CreateCustomerDeliveryAddressArgs `json:"delivery,omitempty"`
	NameOnCard    string                             `json:"nameOnCard"`
}

type CreateCustomerDeliveryAddressArgs struct {
	Type        string  `json:"type"`
	CountryCode string  `json:"countryCode"`
	Line1       string  `json:"line1"`
	Line2       *string `json:"line2,omitempty"`
	Line3       *string `json:"line3,omitempty"`
	City        string  `json:"city"`
	PostOffice  *string `json:"postOffice,omitempty"`
	ZipCode     string  `json:"zipCode"`
	Reason      string  `json:"reason"`
}

type FreezeCardArgs struct {
	ReasonCode string  `json:"-"`
	Note       *string `json:"note,omitempty"`
}

type UnfreezeCardArgs struct {
	Note *string `json:"note,omitempty"`
}

type CloseCardArgs struct {
	ReasonCode string `json:"-"`
}
