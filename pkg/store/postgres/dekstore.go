package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chunlea/marionette/pkg/cryptoutil"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
)

// DEKStore adapts the data_keys table to cryptoutil.DEKStore.
//
// The table and its CRUD have existed since the initial schema, but nothing
// ever implemented the interface the encryption service consumes, so envelope
// encryption only ever ran against an in-memory mock.
type DEKStore struct {
	store *Store
}

var _ cryptoutil.DEKStore = (*DEKStore)(nil)

// NewDEKStore returns a cryptoutil.DEKStore backed by PostgreSQL.
func NewDEKStore(s *Store) *DEKStore {
	return &DEKStore{store: s}
}

// GetDEK retrieves a DEK by resource type and ID.
//
// A missing key is reported as cryptoutil.ErrDEKNotFound rather than the
// store's own not-found error: the service uses that to decide between "create
// the first key for this resource" and "the database is broken", and it cannot
// be expected to know the store's error vocabulary.
func (d *DEKStore) GetDEK(ctx context.Context, resourceType, resourceID string) (*cryptoutil.DataKey, error) {
	key, err := d.store.GetDataKeyByResource(ctx, resourceType, resourceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Wrapped, not bare: callers must match this with errors.Is, and an
			// error that says which resource is missing is worth more in a log
			// than a bare sentinel.
			return nil, fmt.Errorf("%w for %s/%s", cryptoutil.ErrDEKNotFound, resourceType, resourceID)
		}
		return nil, err
	}

	return toCryptoDataKey(key), nil
}

// CreateDEK stores a new encrypted DEK.
//
// Two processes encrypting for the same resource for the first time will both
// try to create one. The insert does nothing on conflict and reports
// ErrDEKExists so the loser adopts the winner's key; without that, one of them
// would keep a DEK that was never persisted and everything it encrypted would
// be unreadable.
func (d *DEKStore) CreateDEK(ctx context.Context, dk *cryptoutil.DataKey) error {
	if dk.ID == "" {
		dk.ID = id.DataKey()
	}

	query := `
		INSERT INTO data_keys (id, resource_type, resource_id, dek_encrypted, algorithm, kek_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (resource_type, resource_id) DO NOTHING`

	var kekID *string
	if dk.KEKID != "" {
		kekID = &dk.KEKID
	}

	result, err := d.store.pool.Exec(ctx, query,
		dk.ID, dk.ResourceType, dk.ResourceID, dk.DEKEncrypted, dk.Algorithm, kekID)
	if err != nil {
		return handlePgError(err, "data_key", dk.ID)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("%w for %s/%s", cryptoutil.ErrDEKExists, dk.ResourceType, dk.ResourceID)
	}

	return nil
}

// UpdateDEK replaces the encrypted key material, for rotation.
//
// KEKID is not written back: store.DataKeyUpdates has no field for it, so a KEK
// rotation cannot yet record which KEK a row is under. See the report — that is
// a gap in the store model, not something to paper over here.
func (d *DEKStore) UpdateDEK(ctx context.Context, dk *cryptoutil.DataKey) error {
	rotatedAt := time.Now()
	updates := store.DataKeyUpdates{
		DEKEncrypted: &dk.DEKEncrypted,
		RotatedAt:    &rotatedAt,
	}

	if err := d.store.UpdateDataKey(ctx, dk.ID, updates); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return cryptoutil.ErrDEKNotFound
		}
		return err
	}
	return nil
}

func toCryptoDataKey(key *store.DataKey) *cryptoutil.DataKey {
	dk := &cryptoutil.DataKey{
		ID:           key.ID,
		ResourceType: key.ResourceType,
		ResourceID:   key.ResourceID,
		DEKEncrypted: key.DEKEncrypted,
		Algorithm:    key.Algorithm,
		CreatedAt:    key.CreatedAt,
		RotatedAt:    key.RotatedAt,
	}
	if key.KEKID != nil {
		dk.KEKID = *key.KEKID
	}
	return dk
}
