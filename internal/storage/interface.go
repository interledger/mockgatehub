package storage

import (
	"encoding/json"

	"mockgatehub/internal/models"
)

// Storage defines the interface for data persistence
type Storage interface {
	// Users
	CreateUser(user *models.User) error
	GetUser(id string) (*models.User, error)
	GetUserByEmail(email string) (*models.User, error)
	UpdateUser(user *models.User) error

	// Card Customers
	CreateCustomer(customer *models.Customer) error
	GetCustomer(id string) (*models.Customer, error)
	GetCustomerBySourceID(sourceID string) (*models.Customer, error)
	UpdateCustomer(customer *models.Customer) error

	// Card Accounts
	CreateAccount(account *models.Account) error
	GetAccount(id string) (*models.Account, error)
	UpdateAccount(account *models.Account) error

	// Card Delivery Addresses
	CreateCustomerAddress(customerID string, address *models.CustomerDeliveryAddress) error
	GetCustomerAddresses(customerID string) ([]*models.CustomerDeliveryAddress, error)

	// Cards
	CreateCard(card *models.Card) error
	GetCard(id string) (*models.Card, error)
	UpdateCard(card *models.Card) error
	GetCardsByCustomer(customerID string) ([]*models.Card, error)
	GetCardsByAccount(accountID string) ([]*models.Card, error)
	GetCardLimits(cardID string) ([]models.CardLimit, error)
	SetCardLimits(cardID string, limits []models.CardLimit) error

	// Card Transactions
	CreateCardTransaction(tx *models.CardTransaction) error
	GetCardTransaction(id string) (*models.CardTransaction, error)
	UpdateCardTransactionStatus(txID string, status string) error
	AddCardTransactionIndex(cardID string, transactionID string) error
	GetCardTransactionIDs(cardID string) ([]string, error)

	// Raw card transactions. A simulated transaction can carry fields the
	// typed model does not know about, and consumers care about those fields,
	// so the original JSON is kept verbatim alongside the typed record.
	StoreRawCardTransaction(txID string, data json.RawMessage) error
	GetRawCardTransaction(txID string) (json.RawMessage, error)

	// Card PINs. Stored separately from the card because a PIN is set through
	// its own encrypted endpoint, not as part of the card object.
	SetCardPIN(cardID string, pin string) error
	GetCardPIN(cardID string) (string, error)

	// Card transaction sequence. GateHub numbers card transactions with a
	// monotonically increasing integer `id` distinct from the transaction UUID.
	NextCardTransactionSeqID() (int, error)
	PeekCardTransactionSeqID() (int, error)

	// Wallets
	CreateWallet(wallet *models.Wallet) error
	GetWallet(address string) (*models.Wallet, error)
	GetWalletsByUser(userID string) ([]*models.Wallet, error)

	// Transactions
	CreateTransaction(tx *models.Transaction) error
	GetTransaction(id string) (*models.Transaction, error)
	UpdateTransactionStatus(id string, status int) error

	// Balances (per user, per currency)
	GetBalance(userID, currency string) (float64, error)
	AddBalance(userID, currency string, amount float64) error
	DeductBalance(userID, currency string, amount float64) error

	// 3DS Challenges
	CreateThreeDSChallenge(challenge *models.ThreeDSChallenge) error
	GetThreeDSChallenge(txID string) (*models.ThreeDSChallenge, error)
	GetPendingThreeDSChallenges(userID string) ([]*models.ThreeDSChallenge, error)
	UpdateThreeDSChallenge(challenge *models.ThreeDSChallenge) error

	// Organizations
	GetOrganization(orgID string) (*models.Organization, error)
	CreateOrganization(org *models.Organization) error
	UpdateOrganization(org *models.Organization) error
}
