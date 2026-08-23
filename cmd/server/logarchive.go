package main

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/config"
	"github.com/chunlea/marionette/pkg/cryptoutil"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/jobs"
	"github.com/chunlea/marionette/pkg/server/api"
	"github.com/chunlea/marionette/pkg/storage/logarchive"
	"github.com/chunlea/marionette/pkg/store/postgres"
)

// logArchiving is everything the log archive feature contributes to the server.
//
// It is one struct because the three parts are one decision. If the archiver
// runs, retrieval has to follow the logs into the archive or the feature reads
// as data loss; and partition retention may only be switched on when something
// is producing the archives it checks for. Wiring them separately is how two of
// the three end up switched on and the third does not.
type logArchiving struct {
	// Objects is the archive codec over the configured blob store, or nil when
	// archiving is off.
	Objects *logarchive.Store

	// Reader serves logs from the archive as well as from PostgreSQL. It is
	// built whenever there is a database, with or without archiving: the
	// session log endpoint answers from the hot rows either way.
	Reader *api.ArchivedLogReader

	// Archiver is the background job, or nil when archiving is off.
	Archiver *jobs.LogArchiver

	// RetentionDays is what the partition maintainer may drop. It is zero
	// unless archiving is actually running, whatever the config file says:
	// retention without archives is just deletion on a timer.
	RetentionDays int
}

// initLogArchiving builds the archive codec, the retrieval reader and the job.
//
// A failure to build the blob store or the crypto service leaves archiving off
// rather than failing startup. A server that cannot archive logs is degraded -
// the logs stay in PostgreSQL, which is where they were - and refusing to boot
// over a background sweep would take the deployment down for it.
func initLogArchiving(
	cfg *config.Config,
	secrets *config.Secrets,
	dbStore *postgres.Store,
	logger *zap.Logger,
) logArchiving {
	if dbStore == nil {
		return logArchiving{}
	}

	archiveCfg := cfg.Storage.LogArchive
	if !archiveCfg.Enabled {
		// No archiving, but session log retrieval still needs a reader; with a
		// nil object store it serves the rows in the database and nothing else.
		return logArchiving{Reader: api.NewArchivedLogReader(dbStore, nil)}
	}

	objects, err := buildLogArchiveStore(cfg, secrets, dbStore)
	if err != nil {
		logger.Error("log archiving is enabled but could not be built; archiving disabled",
			zap.Error(err))
		return logArchiving{Reader: api.NewArchivedLogReader(dbStore, nil)}
	}

	archiver := jobs.NewLogArchiver(dbStore, objects, jobs.LogArchiverConfig{
		Interval:        archiveCfg.Interval,
		TerminatedAfter: archiveCfg.TerminatedAfter,
		IdleAfter:       archiveCfg.IdleAfter,
		Retention:       archiveCfg.Retention,
		SessionsPerRun:  archiveCfg.SessionsPerRun,
		LogBatchSize:    archiveCfg.BatchSize,
		Logger:          logger.Named("log-archiver"),
	})

	logger.Info("log archiving enabled",
		zap.String("storage_provider", cfg.Storage.Provider),
		zap.Duration("interval", archiveCfg.Interval),
		zap.Duration("retention", archiveCfg.Retention),
		zap.Int("partition_retention_days", archiveCfg.RetentionDays),
		zap.Bool("encrypted", objects.Encrypts()),
	)

	return logArchiving{
		Objects:       objects,
		Reader:        api.NewArchivedLogReader(dbStore, objects),
		Archiver:      archiver,
		RetentionDays: archiveCfg.RetentionDays,
	}
}

// buildLogArchiveStore resolves the blob backend and, if configured, the
// encryptor the archive frames go through.
func buildLogArchiveStore(
	cfg *config.Config,
	secrets *config.Secrets,
	dbStore *postgres.Store,
) (*logarchive.Store, error) {
	blobs, err := chunkBlobProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("resolving the object store for log archives: %w", err)
	}

	if !cfg.Storage.Encryption.Enabled {
		return logarchive.New(blobs), nil
	}

	if secrets.EncryptionKey == "" {
		return nil, fmt.Errorf("storage.encryption.enabled is set but MARIONETTE_ENCRYPTION_KEY is not")
	}

	cryptoSvc, err := cryptoutil.NewService(secrets.EncryptionKey, postgres.NewDEKStore(dbStore), id.DataKey)
	if err != nil {
		return nil, fmt.Errorf("building the crypto service for log archives: %w", err)
	}

	return logarchive.New(blobs, logarchive.WithEncryptor(dataKeyEncryptor{crypto: cryptoSvc})), nil
}

// dataKeyEncryptor encrypts archive frames under the tenant's data key.
//
// It is not cas.TenantEncryptor, which compresses before encrypting: the
// archive frames are already zstd, and compressing ciphertext-shaped input a
// second time costs CPU to make the object slightly larger.
type dataKeyEncryptor struct {
	crypto *cryptoutil.Service
}

func (e dataKeyEncryptor) Encrypt(ctx context.Context, tenantID string, plaintext []byte) ([]byte, error) {
	return e.crypto.Encrypt(ctx, "tenant", tenantID, plaintext)
}

func (e dataKeyEncryptor) Decrypt(ctx context.Context, tenantID string, ciphertext []byte) ([]byte, error) {
	return e.crypto.Decrypt(ctx, "tenant", tenantID, ciphertext)
}

var _ logarchive.Encryptor = dataKeyEncryptor{}
