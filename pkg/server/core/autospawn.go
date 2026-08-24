package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// Auto-spawn: ask the session's provider for a runner when there is none.
//
// Task creation allocated a runner by searching the runners that had already
// registered themselves. On a fresh managed deployment - a server whose
// provider is Docker and whose fleet is empty - that search is over zero
// candidates, so the task sat pending forever while the provider whose entire
// job is to make runners was never asked for one. Admin spawn worked; the main
// path did not, which is the same "built but never wired" shape as everything
// else this restart has been unpicking.
//
// What happens here is deliberately only half the job: the spawn is started and
// the task stays honestly pending. The runner boots, connects, and the
// runner-joined trigger dispatches the task through the path that already
// exists. There is no second dispatch path, and no waiting for a boot inside a
// request.
const (
	// AutoSpawnLabel marks a runner this server spawned on a session's behalf.
	// The budget counts them and the pending-spawn guard finds them by it, and
	// it reaches the instance itself (container label, pod label) so an
	// operator looking at the infrastructure can tell too.
	AutoSpawnLabel = "marionette.dev/autospawn"

	// AutoSpawnSessionLabel names the session a runner was spawned for.
	AutoSpawnSessionLabel = "marionette.dev/session"

	// DefaultAutoSpawnMaxRunners caps live auto-spawned runners per provider
	// config. It bounds the blast radius of a bug in the trigger path, so it is
	// deliberately small: a deployment that wants a large fleet configures one.
	DefaultAutoSpawnMaxRunners = 10

	// autoSpawnScanLimit bounds the budget scan. The cap is small, so a page
	// this size answers "is the budget spent" many times over.
	autoSpawnScanLimit = 200

	// autoSpawnBootWindow is how long a spawned runner may stay offline before
	// it stops counting as "still coming".
	//
	// It is the reaper's minimum age on purpose: inside the window the instance
	// is mid-boot and belongs to the session that asked for it, and outside it
	// the reaper owns it and will destroy it. A destroyed runner keeps its row
	// and its offline status forever, so without the window a long-lived
	// deployment's history would fill the budget and auto-spawn would stop.
	autoSpawnBootWindow = DefaultReapMinAge
)

// Session annotations that make a refused or failed spawn visible.
//
// They live on the session rather than in memory because the next attempt can
// land on a different replica, and because "why is my task still pending" is a
// question the API has to be able to answer after a restart.
const (
	autoSpawnErrorAnnotation    = "marionette.dev/autospawn-error"
	autoSpawnRetryAnnotation    = "marionette.dev/autospawn-retry-after"
	autoSpawnAttemptsAnnotation = "marionette.dev/autospawn-attempts"
)

// ErrRunnerSpawning means no runner was available and one is being spawned.
//
// It wraps ErrNoRunnerAvailable so every caller keeps behaving exactly as it
// did - the task stays pending, the backoff advances, the sweeper counts it -
// while the log line says the true thing rather than "no runner available".
var ErrRunnerSpawning = fmt.Errorf("%w: a runner is being spawned", ErrNoRunnerAvailable)

// AutoSpawnPolicy is the effective auto-spawn configuration.
type AutoSpawnPolicy struct {
	// Enabled turns auto-spawn on for managed providers.
	Enabled bool
	// MaxRunners caps live auto-spawned runners per provider config. Zero uses
	// DefaultAutoSpawnMaxRunners; negative means no cap, which nothing should
	// configure but which is at least explicit.
	MaxRunners int
}

func (p AutoSpawnPolicy) maxRunners() int {
	if p.MaxRunners == 0 {
		return DefaultAutoSpawnMaxRunners
	}
	return p.MaxRunners
}

// autoSpawnRunner asks the session's managed provider for a runner, and reports
// whether a spawn was started.
//
// It never returns a runner id. The instance is not connected when Spawn
// returns and would not be for seconds or minutes; activating a session onto it
// would send AttachSession into a stream that does not exist and dispatch a
// task to a runner that cannot receive it.
func (m *SessionManager) autoSpawnRunner(
	ctx context.Context,
	session *store.Session,
	profile *store.Profile,
) bool {
	if !m.autoSpawn.Enabled || m.provisioner == nil || m.providerRegistry == nil {
		return false
	}

	prov, provConfigID, ok := m.managedProviderForSession(ctx, profile)
	if !ok {
		return false
	}

	// Bind the session's tenant even under a background context. The waker and
	// the sweeper run with system access and carry no tenant, and a runner row
	// written without one could never be selected for a tenant's session -
	// selectIdleRunner would skip it on the tenant check, and the spawn would
	// repeat until the budget was spent.
	spawnCtx := ctx
	if session.TenantID != nil && *session.TenantID != "" {
		spawnCtx = store.WithTenant(ctx, *session.TenantID)
	}

	if pending, reason := m.autoSpawnPending(spawnCtx, session); pending {
		m.logger.Debug("not auto-spawning: a runner for this session is already on its way",
			zap.String("session_id", session.ID),
			zap.String("reason", reason),
		)
		return false
	}

	if retryAt, waiting := m.autoSpawnBackoff(session); waiting {
		m.logger.Debug("not auto-spawning: the previous attempt failed and the backoff has not expired",
			zap.String("session_id", session.ID),
			zap.Time("retry_after", retryAt),
		)
		return false
	}

	live := m.countAutoSpawned(spawnCtx, session, provConfigID)
	if max := m.autoSpawn.maxRunners(); max > 0 && live >= max {
		m.recordAutoSpawnFailure(spawnCtx, session,
			fmt.Errorf("auto-spawn budget spent: %d of %d runners on this provider", live, max))
		m.logger.Warn("not auto-spawning: the budget for this provider is spent",
			zap.String("session_id", session.ID),
			zap.String("provider", prov.Name()),
			zap.Int("live_auto_spawned", live),
			zap.Int("max_runners", max),
		)
		return false
	}

	opts := m.autoSpawnOptions(spawnCtx, session, profile, prov, provConfigID)

	runner, err := m.provisioner.Spawn(spawnCtx, opts)
	if err != nil {
		m.recordAutoSpawnFailure(spawnCtx, session, err)
		m.logger.Error("auto-spawn failed; the task stays pending with a recorded reason",
			zap.String("session_id", session.ID),
			zap.String("provider", prov.Name()),
			zap.Error(err),
		)
		return false
	}

	m.clearAutoSpawnFailure(spawnCtx, session)
	m.logger.Info("auto-spawned a runner for a session with nowhere to run",
		zap.String("session_id", session.ID),
		zap.String("runner_id", runner.ID),
		zap.String("provider", prov.Name()),
	)
	return true
}

// autoSpawnOptions describes the instance to spawn.
//
// The session's network policy and the profile's resources are carried through
// deliberately: a session that asked to be air-gapped must not get an
// unrestricted container merely because the server, rather than an operator,
// asked for it.
func (m *SessionManager) autoSpawnOptions(
	ctx context.Context,
	session *store.Session,
	profile *store.Profile,
	prov provider.Provider,
	provConfigID string,
) ProvisionOptions {
	labels := map[string]string{
		AutoSpawnLabel:        "true",
		AutoSpawnSessionLabel: session.ID,
	}

	// A profile's os/arch selector is a filter on allocation, so a runner
	// spawned to satisfy it has to carry the labels it filters on - otherwise
	// the instance boots, is filtered straight back out, and the session spawns
	// again until the budget stops it.
	if selector := m.profileSelector(profile); selector != nil {
		if selector.OS != "" {
			labels["os"] = selector.OS
		}
		if selector.Arch != "" {
			labels["arch"] = selector.Arch
		}
	}

	opts := ProvisionOptions{
		Name:             fmt.Sprintf("auto-%s-%s", session.ID, time.Now().UTC().Format("150405")),
		ProviderConfigID: provConfigID,
		Labels:           labels,
		NetworkPolicy:    session.NetworkPolicy,
		AllowedHosts:     session.AllowedHosts,
	}
	if provConfigID == "" {
		opts.ProviderName = prov.Name()
	}
	if m.workspaceManager != nil {
		opts.WorkspaceMount, _ = m.workspaceManager.GetHostPath(ctx, session.WorkspaceID)
	}

	if profile != nil {
		opts.ProfileID = profile.ID

		if resources, err := parseProfileResources(profile.Resources); err == nil && resources != nil {
			opts.CPUs = float64(resources.CPU)
			opts.MemoryMB = parseMemorySize(resources.Memory)
			opts.DiskMB = parseMemorySize(resources.Disk)
		}
		if network, err := parseProfileNetwork(profile.Network); err == nil && network != nil {
			if network.Level != "" {
				opts.NetworkPolicy = network.Level
			}
			if len(network.AllowedHosts) > 0 {
				opts.AllowedHosts = network.AllowedHosts
			}
		}
	}

	// The claim is what keeps the spawn from being handed to somebody else the
	// moment it connects. It is taken inside Spawn, between writing the runner
	// row and asking the provider for an instance - before anything could
	// possibly connect, let alone be allocated.
	opts.ClaimForSessionID = session.ID
	return opts
}

// managedProviderForSession resolves the provider a session would spawn on.
//
// A profile that names a provider config wins; otherwise it is the registry's
// default, which is what a deployment configured `providers.default` to mean.
// Pool and external providers are not spawnable and are not an error: their
// runners arrive by themselves.
func (m *SessionManager) managedProviderForSession(
	ctx context.Context,
	profile *store.Profile,
) (provider.Provider, string, bool) {
	var (
		prov         provider.Provider
		provConfigID string
		err          error
	)

	if profile != nil && profile.ProviderConfigID != nil && *profile.ProviderConfigID != "" {
		provConfig, cfgErr := m.store.GetProviderConfig(ctx, *profile.ProviderConfigID)
		if cfgErr != nil {
			m.logger.Warn("cannot auto-spawn: the profile's provider config could not be read",
				zap.String("profile_id", profile.ID), zap.Error(cfgErr))
			return nil, "", false
		}
		prov, err = m.providerRegistry.Get(ctx, provConfig.Name)
		provConfigID = provConfig.ID
	} else {
		prov, err = m.providerRegistry.GetDefault(ctx)
	}
	if err != nil || prov == nil {
		m.logger.Debug("cannot auto-spawn: no provider resolved", zap.Error(err))
		return nil, "", false
	}
	if prov.Type() != provider.ProviderTypeManaged {
		m.logger.Debug("not auto-spawning: the session's provider does not spawn runners",
			zap.String("provider", prov.Name()),
			zap.String("type", string(prov.Type())),
		)
		return nil, "", false
	}

	// Resolve the config id by name when the profile did not name one, so the
	// runner row links back to a provider and the budget can be counted per
	// config rather than per deployment.
	if provConfigID == "" {
		if cfg, cfgErr := m.store.GetProviderConfigByName(ctx, prov.Name()); cfgErr == nil {
			provConfigID = cfg.ID
		}
	}

	return prov, provConfigID, true
}

// autoSpawnPending reports whether a runner spawned for this session is still
// on its way or already waiting.
//
// This is what keeps the trigger from spawning once per sweep: the sweeper
// fires every 60 seconds and every runner-joined edge fires too, and a boot
// takes longer than that.
func (m *SessionManager) autoSpawnPending(ctx context.Context, session *store.Session) (bool, string) {
	runners, err := m.store.ListRunners(ctx, store.ListRunnersOptions{
		BaseListOptions: store.BaseListOptions{Limit: autoSpawnScanLimit},
		Labels: map[string]string{
			AutoSpawnLabel:        "true",
			AutoSpawnSessionLabel: session.ID,
		},
	})
	if err != nil {
		// Refusing to spawn on a read failure is the safe side of the trade: a
		// missed spawn is a task that stays pending, a spurious one is an
		// instance nobody asked for that bills until the reaper finds it.
		m.logger.Warn("could not check for a pending auto-spawn; not spawning",
			zap.String("session_id", session.ID), zap.Error(err))
		return true, "the pending-spawn check failed"
	}

	for _, runner := range runners.Items {
		if autoSpawnCounts(runner) {
			return true, fmt.Sprintf("runner %s is %s", runner.ID, runner.Status)
		}
	}
	return false, ""
}

// countAutoSpawned counts the live auto-spawned runners on a provider config.
func (m *SessionManager) countAutoSpawned(
	ctx context.Context,
	session *store.Session,
	provConfigID string,
) int {
	runners, err := m.store.ListRunners(ctx, store.ListRunnersOptions{
		BaseListOptions: store.BaseListOptions{Limit: autoSpawnScanLimit},
		Labels:          map[string]string{AutoSpawnLabel: "true"},
	})
	if err != nil {
		m.logger.Warn("could not count auto-spawned runners; treating the budget as spent",
			zap.Error(err))
		return m.autoSpawn.maxRunners()
	}
	if runners.HasMore {
		m.logger.Warn("more auto-spawned runners than one budget scan can see",
			zap.Int("scan_limit", autoSpawnScanLimit))
	}

	count := 0
	for _, runner := range runners.Items {
		// The background triggers run with system access, which sees every
		// tenant's rows: without this, one tenant's fleet would exhaust
		// another's budget.
		if !sameTenant(session.TenantID, runner.TenantID) {
			continue
		}
		if runnerProviderConfigID(runner) != provConfigID {
			continue
		}
		if autoSpawnCounts(runner) {
			count++
		}
	}
	return count
}

// autoSpawnCounts reports whether a runner should count against the budget.
//
// Connected runners always count. An offline one counts only while it could
// still be booting: Destroy leaves the row behind with status offline, so
// counting every offline row would let a deployment's history spend a budget
// that is supposed to describe what is running right now.
func autoSpawnCounts(runner *store.Runner) bool {
	switch runner.Status {
	case StatusIdle, StatusBusy, StatusPaused:
		return true
	case StatusOffline:
		return time.Since(runner.CreatedAt) < autoSpawnBootWindow
	default:
		return false
	}
}

func runnerProviderConfigID(runner *store.Runner) string {
	if runner.ProviderConfigID == nil {
		return ""
	}
	return *runner.ProviderConfigID
}

// autoSpawnBackoff reports when the next attempt is allowed, and whether that
// is still in the future.
func (m *SessionManager) autoSpawnBackoff(session *store.Session) (time.Time, bool) {
	annotations := decodeAnnotations(session.Annotations)
	raw, ok := annotations[autoSpawnRetryAnnotation]
	if !ok || raw == "" {
		return time.Time{}, false
	}

	retryAt, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return retryAt, time.Now().Before(retryAt)
}

// recordAutoSpawnFailure makes a refusal visible and schedules the next try.
//
// The backoff schedule is the redispatch one, deliberately: a spawn that fails
// because the Docker daemon is down fails the same way for every session, and
// retrying it every sweep would be a hot loop against something already broken.
func (m *SessionManager) recordAutoSpawnFailure(ctx context.Context, session *store.Session, cause error) {
	annotations := decodeAnnotations(session.Annotations)

	attempts := 0
	if raw, ok := annotations[autoSpawnAttemptsAnnotation]; ok {
		if parsed, err := strconv.Atoi(raw); err == nil {
			attempts = parsed
		}
	}
	attempts++

	retryAt := time.Now().Add(redispatchBackoff(attempts))
	annotations[autoSpawnErrorAnnotation] = cause.Error()
	annotations[autoSpawnRetryAnnotation] = retryAt.UTC().Format(time.RFC3339)
	annotations[autoSpawnAttemptsAnnotation] = strconv.Itoa(attempts)

	m.writeAnnotations(ctx, session, annotations)
}

// clearAutoSpawnFailure removes the failure annotations after a spawn worked.
func (m *SessionManager) clearAutoSpawnFailure(ctx context.Context, session *store.Session) {
	annotations := decodeAnnotations(session.Annotations)
	if _, ok := annotations[autoSpawnErrorAnnotation]; !ok {
		if _, retrying := annotations[autoSpawnRetryAnnotation]; !retrying {
			return
		}
	}

	delete(annotations, autoSpawnErrorAnnotation)
	delete(annotations, autoSpawnRetryAnnotation)
	delete(annotations, autoSpawnAttemptsAnnotation)
	m.writeAnnotations(ctx, session, annotations)
}

func (m *SessionManager) writeAnnotations(
	ctx context.Context,
	session *store.Session,
	annotations map[string]string,
) {
	encoded, err := json.Marshal(annotations)
	if err != nil {
		m.logger.Warn("could not encode session annotations",
			zap.String("session_id", session.ID), zap.Error(err))
		return
	}

	// Not cancelled with the caller: this is the record of why a task is still
	// pending, and losing it because a request returned is how a session ends
	// up stuck with no explanation.
	if err := m.store.UpdateSession(context.WithoutCancel(ctx), session.ID, store.SessionUpdates{
		Annotations: encoded,
	}); err != nil && !errors.Is(err, store.ErrNotFound) {
		m.logger.Warn("could not record the auto-spawn outcome on the session",
			zap.String("session_id", session.ID), zap.Error(err))
		return
	}
	session.Annotations = encoded
}

// decodeAnnotations reads a session's annotations, treating anything
// unreadable as empty rather than as a reason to fail.
func decodeAnnotations(raw json.RawMessage) map[string]string {
	annotations := map[string]string{}
	if len(raw) == 0 {
		return annotations
	}
	if err := json.Unmarshal(raw, &annotations); err != nil {
		return map[string]string{}
	}
	if annotations == nil {
		// JSON null unmarshals into a nil map without erroring, and a nil map
		// panics on the first write. The column is nullable, so this is the
		// ordinary case for a session nothing has annotated.
		return map[string]string{}
	}
	return annotations
}
