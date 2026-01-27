package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mockgatehub/internal/auth"
	"mockgatehub/internal/config"
	"mockgatehub/internal/handler"
	"mockgatehub/internal/logger"
	"mockgatehub/internal/storage"
	"mockgatehub/internal/webhook"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	logger.Info.Println("Starting MockGatehub...")

	cfg := config.Load()
	logger.Info.Printf("Configuration: Port=%s, UseRedis=%v", cfg.Port, cfg.UseRedis)

	var store storage.Storage
	if cfg.UseRedis {
		logger.Info.Printf("Using Redis storage: %s (DB: %d)", cfg.RedisURL, cfg.RedisDB)
		redisStore, err := storage.NewRedisStorage(cfg.RedisURL, cfg.RedisDB)
		if err != nil {
			logger.Error.Fatalf("Failed to connect to Redis: %v", err)
		}
		defer redisStore.Close()
		store = redisStore
	} else {
		logger.Info.Println("Using in-memory storage")
		store = storage.NewMemoryStorage()
	}

	if err := storage.SeedTestUsers(store); err != nil {
		logger.Error.Fatalf("Failed to seed test users: %v", err)
	}

	// Initialize webhook queue and worker
	var webhookQueue *webhook.Queue
	var webhookWorker *webhook.Worker

	if cfg.UseRedis {
		// Use Redis-backed queue for webhook delivery
		redisStore, ok := store.(*storage.RedisStorage)
		if !ok {
			logger.Error.Fatalf("Redis storage type assertion failed")
		}
		webhookQueue = webhook.NewQueue(redisStore.GetClient())
		logger.Info.Println("Using Redis-backed webhook queue")
	} else {
		// For in-memory mode, we still need Redis for webhook queue
		// Create a dedicated Redis connection just for webhooks
		logger.Warn.Println("WARNING: In-memory storage mode requires Redis for webhook queue")
		logger.Warn.Printf("Connecting to Redis for webhook queue: %s (DB: %d)", cfg.RedisURL, cfg.RedisDB)
		redisClient, err := storage.NewRedisClient(cfg.RedisURL, cfg.RedisDB)
		if err != nil {
			logger.Error.Fatalf("Failed to connect to Redis for webhook queue: %v", err)
		}
		webhookQueue = webhook.NewQueue(redisClient)
	}

	webhookManager := webhook.NewManager(cfg.WebhookURL, cfg.WebhookSecret, webhookQueue)
	webhookWorker = webhook.NewWorker(webhookQueue, webhookManager)

	// Start webhook worker in background
	webhookWorker.StartAsync()
	logger.Info.Println("Webhook worker started")
	h := handler.NewHandler(store, webhookManager)
	r := chi.NewRouter()

	// Built-in middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	// Custom request logger with detailed output
	r.Use(func(next http.Handler) http.Handler {
		return h.RequestLogger(next)
	})

	// Authentication middleware (applied to protected routes)
	if cfg.EnforceAuthentication {
		logger.Info.Printf("Authentication enforcement ENABLED. Valid app IDs: %v", cfg.ValidCredentials)
		authMiddleware := auth.Middleware(cfg.ValidCredentials)
		r.Use(authMiddleware)
	} else {
		logger.Info.Println("WARNING: Authentication enforcement DISABLED")
	}

	setupRoutes(r, h)

	// Log unmatched routes to surface any misrouted traffic
	r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info.Printf("[NOTFOUND] %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		http.NotFound(w, r)
	}))

	// Log method-not-allowed for visibility
	r.MethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info.Printf("[METHODNOTALLOWED] %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}))

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info.Printf("MockGatehub listening on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error.Fatalf("Server failed to start: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info.Println("Shutting down server...")

	// Stop webhook worker
	if webhookWorker != nil {
		logger.Info.Println("Stopping webhook worker...")
		webhookWorker.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error.Fatalf("Server forced to shutdown: %v", err)
	}

	logger.Info.Println("Server stopped")
}

func setupRoutes(r chi.Router, h *handler.Handler) {
	r.Get("/", h.RootHandler)
	r.Post("/transaction/complete", h.TransactionCompleteHandler)
	r.Get("/health", h.HealthCheck)
	r.Get("/api/user-currencies", h.GetUserCurrencies)
	r.Route("/auth/v1", func(r chi.Router) {
		r.Post("/tokens", h.CreateToken)
		r.Post("/users/managed", h.CreateManagedUser)
		r.Get("/users/managed", h.GetManagedUser)
		r.Put("/users/managed/email", h.UpdateManagedUserEmail)
	})
	r.Route("/id/v1", func(r chi.Router) {
		r.Get("/users/{userID}", h.GetUser)
		r.Post("/users/{userID}/hubs/{gatewayID}", h.StartKYC)
		r.Put("/hubs/{gatewayID}/users/{userID}", h.UpdateKYCState)
		r.Post("/hubs/{gatewayID}/users/{userID}/overrideRiskLevel", h.OverrideRiskLevel)
	})
	r.Get("/iframe/onboarding", h.KYCIframe)
	r.Post("/iframe/submit", h.KYCIframeSubmit)
	r.Route("/core/v1", func(r chi.Router) {
		r.Get("/users/{userID}", h.GetUserWallets)
		r.Post("/users/{userID}/wallets", h.CreateWallet)
		r.Get("/users/{userID}/wallets/{walletID}", h.GetWallet)
		r.Get("/wallets/{walletID}/balances", h.GetWalletBalance)
		r.Post("/transactions", h.CreateTransaction)
		r.Get("/transactions/{txID}", h.GetTransaction)
	})
	r.Route("/rates/v1", func(r chi.Router) {
		r.Get("/rates/current", h.GetCurrentRates)
		r.Get("/liquidity_provider/vaults", h.GetVaults)
	})
	r.Route("/cards/v1", func(r chi.Router) {
		r.Post("/customers", h.CreateManagedCustomer)
		r.Get("/cards/{customerID}", h.ListCards)
		r.Get("/cards/{cardID}/card", h.GetCard)
		r.Get("/cards/{cardID}/transactions", h.GetCardTransactions)
		r.Get("/cards/{cardID}/limits", h.GetCardLimits)
		r.Put("/cards/{cardID}/limits", h.SetCardLimits)
		r.Put("/cards/{cardID}/lock", h.LockCard)
		r.Put("/cards/{cardID}/unlock", h.UnlockCard)
		r.Put("/cards/{cardID}/block", h.BlockCard)
		r.Delete("/cards/{cardID}/card", h.DeleteCard)
		r.Post("/token/{tokenType}", h.GetCardToken)
		r.Get("/token/{tokenType}/data", h.GetTokenData)
		r.Post("/pin/change", h.ChangePin)
		r.Post("/transactions", h.CreateCardTransaction)
		r.Get("/transactions/{txID}", h.GetCardTransaction)
		r.Get("/transaction/pending-confirmations", h.GetPendingConfirmations)
		r.Get("/customers/{customerID}/addresses", h.GetDeliveryAddresses)
		r.Post("/customers/{customerID}/addresses", h.CreateCustomerDeliveryAddress)
		r.Post("/cards/{accountID}/card", h.OrderCard)
	})
}
