package storage

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"mockgatehub/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storesUnderTest returns each Storage implementation with a clean state, so
// the same behavioural expectations are checked against both. Redis is skipped
// when unavailable rather than failing the suite.
func storesUnderTest(t *testing.T) map[string]Storage {
	t.Helper()
	stores := map[string]Storage{"memory": NewMemoryStorage()}

	if redisStore, err := NewRedisStorage("redis://localhost:6379", 15); err == nil {
		require.NoError(t, redisStore.client.FlushDB(redisStore.ctx).Err())
		t.Cleanup(func() { _ = redisStore.Close() })
		stores["redis"] = redisStore
	} else {
		t.Log("redis unavailable; only exercising in-memory storage")
	}
	return stores
}

// seedCardForPIN creates the card a PIN can be attached to. Storage refuses a
// PIN for a card it does not know about.
func seedCardForPIN(t *testing.T, store Storage, cardID string) {
	t.Helper()
	require.NoError(t, store.CreateCard(&models.Card{
		ID:        cardID,
		AccountID: "account-" + cardID,
		Status:    "Active",
	}))
}

func TestRawCardTransaction_RoundTripsUnmodelledFields(t *testing.T) {
	for name, store := range storesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			// A field the typed model knows nothing about must still come back,
			// because that is the whole reason the raw payload is kept.
			raw := json.RawMessage(`{"transactionId":"tx-1","aFieldWeDoNotModel":"keep me","nested":{"a":1}}`)
			require.NoError(t, store.StoreRawCardTransaction("tx-1", raw))

			got, err := store.GetRawCardTransaction("tx-1")
			require.NoError(t, err)

			var fields map[string]interface{}
			require.NoError(t, json.Unmarshal(got, &fields))
			assert.Equal(t, "keep me", fields["aFieldWeDoNotModel"])
			assert.NotNil(t, fields["nested"])
		})
	}
}

func TestGetRawCardTransaction_MissingIsAnError(t *testing.T) {
	for name, store := range storesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			_, err := store.GetRawCardTransaction("never-stored")
			assert.Error(t, err)
		})
	}
}

func TestStoreRawCardTransaction_RequiresATransactionID(t *testing.T) {
	for name, store := range storesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, store.StoreRawCardTransaction("", json.RawMessage(`{}`)))
		})
	}
}

func TestStoreRawCardTransaction_IsNotAliasedToTheCallersBuffer(t *testing.T) {
	// Storage that kept the caller's slice would silently change under it.
	store := NewMemoryStorage()
	buf := []byte(`{"txStatus":"PROCESSING"}`)
	require.NoError(t, store.StoreRawCardTransaction("tx-1", buf))

	copy(buf, []byte(`{"txStatus":"XXXXXXXXXX"}`))

	got, err := store.GetRawCardTransaction("tx-1")
	require.NoError(t, err)
	assert.JSONEq(t, `{"txStatus":"PROCESSING"}`, string(got))
}

func TestUpdateCardTransactionStatus_UpdatesBothViews(t *testing.T) {
	for name, store := range storesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			processing := "PROCESSING"
			require.NoError(t, store.CreateCardTransaction(&models.CardTransaction{
				TransactionID: "tx-1",
				TxStatus:      &processing,
			}))
			require.NoError(t, store.StoreRawCardTransaction("tx-1",
				json.RawMessage(`{"transactionId":"tx-1","txStatus":"PROCESSING","mcc":"5399"}`)))

			require.NoError(t, store.UpdateCardTransactionStatus("tx-1", "COMPLETED"))

			typed, err := store.GetCardTransaction("tx-1")
			require.NoError(t, err)
			require.NotNil(t, typed.TxStatus)
			assert.Equal(t, "COMPLETED", *typed.TxStatus)

			// The raw payload is what readers are served, so a stale status
			// there would be the one consumers actually see.
			raw, err := store.GetRawCardTransaction("tx-1")
			require.NoError(t, err)
			var fields map[string]interface{}
			require.NoError(t, json.Unmarshal(raw, &fields))
			assert.Equal(t, "COMPLETED", fields["txStatus"])
			assert.Equal(t, "5399", fields["mcc"], "unrelated fields must survive the update")
		})
	}
}

func TestUpdateCardTransactionStatus_MissingTransactionIsAnError(t *testing.T) {
	for name, store := range storesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, store.UpdateCardTransactionStatus("nope", "COMPLETED"))
		})
	}
}

func TestCardTransactionSeqID_IncreasesAndPeekDoesNot(t *testing.T) {
	for name, store := range storesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			start, err := store.PeekCardTransactionSeqID()
			require.NoError(t, err)

			first, err := store.NextCardTransactionSeqID()
			require.NoError(t, err)
			second, err := store.NextCardTransactionSeqID()
			require.NoError(t, err)

			assert.Equal(t, start+1, first)
			assert.Equal(t, start+2, second)

			// Peeking must not consume an id, or a caller checking the counter
			// would create gaps in the sequence.
			peeked, err := store.PeekCardTransactionSeqID()
			require.NoError(t, err)
			assert.Equal(t, second, peeked)
		})
	}
}

func TestCardTransactionSeqID_NeverHandsOutADuplicate(t *testing.T) {
	// Two concurrent simulations must not receive the same id.
	store := NewMemoryStorage()
	const workers = 50

	var wg sync.WaitGroup
	ids := make([]int, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			id, err := store.NextCardTransactionSeqID()
			require.NoError(t, err)
			ids[slot] = id
		}(i)
	}
	wg.Wait()

	seen := make(map[int]bool, workers)
	for _, id := range ids {
		assert.False(t, seen[id], "id %d was handed out twice", id)
		seen[id] = true
	}
	assert.Len(t, seen, workers)
}

func TestCardPIN_SetThenGet(t *testing.T) {
	for name, store := range storesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			seedCardForPIN(t, store, "card-1")

			_, err := store.GetCardPIN("card-1")
			assert.Error(t, err, "a card with no PIN set must not report one")

			require.NoError(t, store.SetCardPIN("card-1", "4321"))
			pin, err := store.GetCardPIN("card-1")
			require.NoError(t, err)
			assert.Equal(t, "4321", pin)

			// A later change replaces the value rather than accumulating.
			require.NoError(t, store.SetCardPIN("card-1", "9876"))
			pin, err = store.GetCardPIN("card-1")
			require.NoError(t, err)
			assert.Equal(t, "9876", pin)
		})
	}
}

func TestCardPIN_RejectsUnknownCardAndEmptyID(t *testing.T) {
	for name, store := range storesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			// Accepting a PIN for a card that does not exist would leave an
			// orphan credential behind.
			assert.Error(t, store.SetCardPIN("no-such-card", "1234"))
			assert.Error(t, store.SetCardPIN("", "1234"))
		})
	}
}

func TestCardPIN_IsPerCard(t *testing.T) {
	for name, store := range storesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			seedCardForPIN(t, store, "card-a")
			seedCardForPIN(t, store, "card-b")

			require.NoError(t, store.SetCardPIN("card-a", "1111"))
			require.NoError(t, store.SetCardPIN("card-b", "2222"))

			a, err := store.GetCardPIN("card-a")
			require.NoError(t, err)
			b, err := store.GetCardPIN("card-b")
			require.NoError(t, err)

			assert.Equal(t, "1111", a)
			assert.Equal(t, "2222", b)
		})
	}
}

func TestRawCardTransaction_IsPerTransaction(t *testing.T) {
	for name, store := range storesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			for i := 1; i <= 3; i++ {
				txID := fmt.Sprintf("tx-%d", i)
				payload := fmt.Sprintf(`{"transactionId":%q,"id":%d}`, txID, i)
				require.NoError(t, store.StoreRawCardTransaction(txID, json.RawMessage(payload)))
			}

			for i := 1; i <= 3; i++ {
				txID := fmt.Sprintf("tx-%d", i)
				raw, err := store.GetRawCardTransaction(txID)
				require.NoError(t, err)

				var fields map[string]interface{}
				require.NoError(t, json.Unmarshal(raw, &fields))
				assert.Equal(t, txID, fields["transactionId"])
				assert.EqualValues(t, i, fields["id"])
			}
		})
	}
}

func TestListTransactionsByUser_ReturnsOnlyThatUsersTransactions(t *testing.T) {
	for name, store := range storesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, store.CreateTransaction(&models.Transaction{
				ID: "tx-a", UserID: "user-1", Amount: "1.00", Currency: "EUR",
			}))
			require.NoError(t, store.CreateTransaction(&models.Transaction{
				ID: "tx-b", UserID: "user-2", Amount: "2.00", Currency: "EUR",
			}))
			require.NoError(t, store.CreateTransaction(&models.Transaction{
				ID: "tx-c", UserID: "user-1", Amount: "3.00", Currency: "EUR",
			}))

			got, err := store.ListTransactionsByUser("user-1")
			require.NoError(t, err)

			ids := make([]string, 0, len(got))
			for _, tx := range got {
				ids = append(ids, tx.ID)
			}
			assert.ElementsMatch(t, []string{"tx-a", "tx-c"}, ids,
				"one user's statement must not show another's transactions")
		})
	}
}

func TestListTransactionsByUser_IsMostRecentFirst(t *testing.T) {
	for name, store := range storesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
			// Insert out of order so the ordering cannot come from insertion.
			for _, spec := range []struct {
				id string
				at time.Time
			}{
				{"tx-middle", base.AddDate(0, 0, 1)},
				{"tx-oldest", base},
				{"tx-newest", base.AddDate(0, 0, 2)},
			} {
				require.NoError(t, store.CreateTransaction(&models.Transaction{
					ID: spec.id, UserID: "ordered-user", Amount: "1.00", Currency: "EUR",
					CreatedAt: spec.at,
				}))
			}

			got, err := store.ListTransactionsByUser("ordered-user")
			require.NoError(t, err)
			require.Len(t, got, 3)

			for i := 1; i < len(got); i++ {
				assert.False(t, got[i].CreatedAt.After(got[i-1].CreatedAt),
					"expected newest first, but %s precedes %s", got[i-1].ID, got[i].ID)
			}
		})
	}
}

func TestListTransactionsByUser_UnknownUserIsEmptyNotAnError(t *testing.T) {
	for name, store := range storesUnderTest(t) {
		t.Run(name, func(t *testing.T) {
			got, err := store.ListTransactionsByUser("nobody")
			require.NoError(t, err, "an account with no history is not an error")
			assert.Empty(t, got)
		})
	}
}
