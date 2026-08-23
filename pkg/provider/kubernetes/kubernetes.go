// Package kubernetes implements a Kubernetes pod provider for Marionette.
// It manages runner lifecycle using the Kubernetes API, supporting pod
// creation, destruction, and PersistentVolumeClaim management for workspaces.
package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mnet "github.com/chunlea/marionette/pkg/network"
	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
)

const (
	// Label keys used for pod identification.
	labelManagedBy = "managed-by"
	labelRunnerID  = "runner-id"
	labelTenantID  = "tenant-id"

	// Default pod termination grace period in seconds.
	defaultTerminationGracePeriodSeconds int64 = 30

	// Workspace mount path in the container.
	workspaceMountPath = "/workspace"
)

// Provider implements the Kubernetes pod provider.
type Provider struct {
	name          string
	config        *Config
	suspendConfig *provider.SuspendConfig
	client        KubeClient

	// namespaceOnce ensures namespace check is only done once.
	namespaceOnce sync.Once
	namespaceErr  error

	// resolver pins the hostnames in a network policy to addresses.
	// NetworkPolicy has no notion of a hostname, so this is what makes
	// allow_list expressible at all.
	resolver *mnet.DNSResolver
}

// Compile-time interface checks.
var (
	_ provider.Provider            = (*Provider)(nil)
	_ provider.SuspendableProvider = (*Provider)(nil)
)

// New creates a Kubernetes provider from a store.ProviderConfig.
func New(cfg *store.ProviderConfig) (*Provider, error) {
	k8sCfg, err := ParseConfig(cfg.Config)
	if err != nil {
		return nil, err
	}

	suspendCfg, err := provider.ParseSuspendConfig(cfg.SuspendConfig, defaultSuspendConfig())
	if err != nil {
		return nil, err
	}

	client, err := NewKubeClient(k8sCfg)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	return &Provider{
		name:          cfg.Name,
		config:        k8sCfg,
		suspendConfig: suspendCfg,
		client:        client,
		resolver:      mnet.NewDNSResolver(),
	}, nil
}

// NewWithClient creates a provider with an injected client (for testing).
func NewWithClient(name string, cfg *Config, suspendCfg *provider.SuspendConfig, client KubeClient) *Provider {
	if suspendCfg == nil {
		suspendCfg = &provider.SuspendConfig{}
		suspendCfg.ApplyDefaults(defaultSuspendConfig())
	}
	return &Provider{
		name:          name,
		config:        cfg,
		suspendConfig: suspendCfg,
		client:        client,
		resolver:      mnet.NewDNSResolver(),
	}
}

// Name returns the provider config name.
func (p *Provider) Name() string {
	return p.name
}

// Type returns the provider type (managed).
func (p *Provider) Type() provider.ProviderType {
	return provider.ProviderTypeManaged
}

// Capabilities returns the provider's capabilities.
func (p *Provider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{
		Pause:    false, // Kubernetes doesn't support pod pause
		Snapshot: false, // No native snapshot support
		Suspend: provider.SuspendCapability{
			// Derived from the dispatcher so capabilities cannot claim a
			// strategy the provider does not implement.
			Strategies: p.suspendDispatcher().Strategies(),
			Default:    provider.SuspendStrategyTerminatePreserveStorage,
		},
	}
}

// Spawn creates and starts a new pod with an associated PVC for workspace storage.
func (p *Provider) Spawn(ctx context.Context, opts provider.SpawnOptions) (*provider.RunnerInstance, error) {
	// Verify namespace exists
	if err := p.ensureNamespace(ctx); err != nil {
		return nil, &provider.ErrSpawnFailed{Reason: "namespace verification failed", Cause: err}
	}

	// Generate resource names
	podName := p.podName(opts.Name, opts.RunnerID)
	pvcName := p.pvcName(opts.RunnerID)

	// Create or verify PVC exists
	pvc, err := p.ensurePVC(ctx, pvcName, opts)
	if err != nil {
		return nil, &provider.ErrSpawnFailed{Reason: "PVC creation failed", Cause: err}
	}

	// The NetworkPolicy is created before the pod, not after it.
	//
	// It selects on the runner-id label, so it is already in the API server
	// when the pod carrying that label appears and the CNI has it to program
	// from the start. Creating it afterwards, which is what this used to do,
	// meant the pod ran unrestricted for the whole of waitForPodReady: up to
	// two minutes of unfiltered egress, and no isolation whatsoever for a
	// short-lived task.
	policy, err := p.prepareNetworkPolicy(opts)
	if err != nil {
		return nil, &provider.ErrSpawnFailed{Reason: "invalid network policy", Cause: err}
	}

	var resolved *mnet.ResolvedPolicy
	if policy.IsRestricted() {
		resolved, err = p.createNetworkPolicy(ctx, opts.RunnerID, opts, policy)
		if err != nil {
			return nil, &provider.ErrSpawnFailed{Reason: "network policy creation failed", Cause: err}
		}
	}

	// Build and create pod
	pod := p.buildPod(podName, pvcName, opts, resolved)
	createdPod, err := p.client.CreatePod(ctx, p.config.Namespace, pod)
	if err != nil {
		p.deleteNetworkPolicy(ctx, opts.RunnerID)
		return nil, &provider.ErrSpawnFailed{Reason: "pod creation failed", Cause: err}
	}

	// Wait for pod to be running or fail fast
	if err := p.waitForPodReady(ctx, createdPod.Name, 2*time.Minute); err != nil {
		// Cleanup on failure
		_ = p.client.DeletePod(ctx, p.config.Namespace, createdPod.Name, metav1.DeleteOptions{})
		p.deleteNetworkPolicy(ctx, opts.RunnerID)
		return nil, &provider.ErrSpawnFailed{Reason: "pod startup failed", Cause: err}
	}

	return &provider.RunnerInstance{
		ID:          opts.RunnerID,
		ProviderID:  string(createdPod.UID),
		Name:        opts.Name,
		Status:      provider.InstanceStatusRunning,
		SandboxMode: opts.SandboxMode,
		CreatedAt:   createdPod.CreationTimestamp.Time,
		Labels:      opts.Labels,
		Annotations: opts.Annotations,
		Metadata: map[string]string{
			"pod_name":   createdPod.Name,
			"pod_uid":    string(createdPod.UID),
			"namespace":  p.config.Namespace,
			"pvc_name":   pvc.Name,
			"image":      p.config.Image,
			"node_name":  createdPod.Spec.NodeName,
			"cluster_ip": createdPod.Status.PodIP,
		},
	}, nil
}

// Destroy deletes a pod. By default, it preserves the PVC for workspace persistence.
func (p *Provider) Destroy(ctx context.Context, runnerID string) error {
	return p.destroyPod(ctx, runnerID, false)
}

// destroyPod deletes a pod and optionally its PVC.
func (p *Provider) destroyPod(ctx context.Context, runnerID string, deletePVC bool) error {
	podName, err := p.findPodNameByRunnerID(ctx, runnerID)
	if err != nil {
		return err
	}

	p.deleteNetworkPolicy(ctx, runnerID)

	// Delete pod with grace period
	gracePeriod := defaultTerminationGracePeriodSeconds
	if err := p.client.DeletePod(ctx, p.config.Namespace, podName, metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
	}); err != nil && !k8serrors.IsNotFound(err) {
		return &provider.ErrDestroyFailed{RunnerID: runnerID, Cause: err}
	}

	// Optionally delete PVC
	if deletePVC {
		pvcName := p.pvcName(runnerID)
		if err := p.client.DeletePVC(ctx, p.config.Namespace, pvcName, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
			return &provider.ErrDestroyFailed{RunnerID: runnerID, Cause: fmt.Errorf("deleting PVC: %w", err)}
		}
	}

	return nil
}

// Status returns the current status of a runner.
func (p *Provider) Status(ctx context.Context, runnerID string) (*provider.RunnerStatus, error) {
	podName, err := p.findPodNameByRunnerID(ctx, runnerID)
	if err != nil {
		return nil, err
	}

	pod, err := p.client.GetPod(ctx, p.config.Namespace, podName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, &provider.ErrRunnerNotFound{RunnerID: runnerID}
		}
		return nil, fmt.Errorf("getting pod: %w", err)
	}

	return &provider.RunnerStatus{
		Status:    mapPodPhase(pod.Status.Phase),
		UpdatedAt: time.Now(),
	}, nil
}

// List returns all runners managed by this provider.
func (p *Provider) List(ctx context.Context) ([]*provider.RunnerInstance, error) {
	pods, err := p.client.ListPods(ctx, p.config.Namespace, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s/%s=marionette", p.config.LabelPrefix, labelManagedBy),
	})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}

	instances := make([]*provider.RunnerInstance, 0, len(pods.Items))
	for _, pod := range pods.Items {
		instances = append(instances, p.podToInstance(&pod))
	}

	return instances, nil
}

// SuspendConfig returns the provider's suspend configuration.
func (p *Provider) SuspendConfig() provider.SuspendConfig {
	return *p.suspendConfig
}

// Helper methods

func (p *Provider) podName(name, runnerID string) string {
	if name != "" {
		return fmt.Sprintf("marionette-%s", sanitizeName(name))
	}
	return fmt.Sprintf("marionette-%s", sanitizeName(runnerID))
}

func (p *Provider) pvcName(runnerID string) string {
	return fmt.Sprintf("marionette-ws-%s", sanitizeName(runnerID))
}

// deleteNetworkPolicy removes a runner's NetworkPolicy, ignoring a missing one.
func (p *Provider) deleteNetworkPolicy(ctx context.Context, runnerID string) {
	_ = p.client.DeleteNetworkPolicy(ctx, p.config.Namespace, p.networkPolicyName(runnerID), metav1.DeleteOptions{})
}

func (p *Provider) networkPolicyName(runnerID string) string {
	return fmt.Sprintf("marionette-np-%s", sanitizeName(runnerID))
}

func (p *Provider) labelKey(key string) string {
	return fmt.Sprintf("%s/%s", p.config.LabelPrefix, key)
}

func (p *Provider) ensureNamespace(ctx context.Context) error {
	p.namespaceOnce.Do(func() {
		_, p.namespaceErr = p.client.GetNamespace(ctx, p.config.Namespace)
		if p.namespaceErr != nil {
			if k8serrors.IsNotFound(p.namespaceErr) {
				p.namespaceErr = fmt.Errorf("namespace %s does not exist", p.config.Namespace)
			} else {
				p.namespaceErr = fmt.Errorf("checking namespace: %w", p.namespaceErr)
			}
		}
	})
	return p.namespaceErr
}

func (p *Provider) ensurePVC(ctx context.Context, pvcName string, opts provider.SpawnOptions) (*corev1.PersistentVolumeClaim, error) {
	// Check if PVC already exists
	existingPVC, err := p.client.GetPVC(ctx, p.config.Namespace, pvcName)
	if err == nil {
		// PVC exists, verify it's compatible
		return existingPVC, nil
	}
	if !k8serrors.IsNotFound(err) {
		return nil, fmt.Errorf("checking PVC: %w", err)
	}

	// Create new PVC
	pvc := p.buildPVC(pvcName, opts)
	createdPVC, err := p.client.CreatePVC(ctx, p.config.Namespace, pvc)
	if err != nil {
		return nil, fmt.Errorf("creating PVC: %w", err)
	}

	return createdPVC, nil
}

func (p *Provider) buildPVC(name string, opts provider.SpawnOptions) *corev1.PersistentVolumeClaim {
	labels := map[string]string{
		p.labelKey(labelManagedBy): "marionette",
		p.labelKey(labelRunnerID):  opts.RunnerID,
	}
	if opts.TenantID != "" {
		labels[p.labelKey(labelTenantID)] = opts.TenantID
	}

	// Merge with config labels
	maps.Copy(labels, p.config.Labels)

	storageSize := p.config.Storage.Size
	if opts.DiskMB > 0 {
		storageSize = fmt.Sprintf("%dMi", opts.DiskMB)
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   p.config.Namespace,
			Labels:      labels,
			Annotations: p.config.Annotations,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.PersistentVolumeAccessMode(p.config.Storage.AccessMode),
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}

	if p.config.Storage.StorageClass != "" {
		pvc.Spec.StorageClassName = &p.config.Storage.StorageClass
	}

	return pvc
}

func (p *Provider) buildPod(name, pvcName string, opts provider.SpawnOptions, resolved *mnet.ResolvedPolicy) *corev1.Pod {
	labels := map[string]string{
		p.labelKey(labelManagedBy): "marionette",
		p.labelKey(labelRunnerID):  opts.RunnerID,
	}
	if opts.TenantID != "" {
		labels[p.labelKey(labelTenantID)] = opts.TenantID
	}

	// Merge with opts labels and config labels
	maps.Copy(labels, p.config.Labels)
	maps.Copy(labels, opts.Labels)

	annotations := make(map[string]string)
	maps.Copy(annotations, p.config.Annotations)
	maps.Copy(annotations, opts.Annotations)

	// Build environment variables. Proxy mode is enforced by the
	// NetworkPolicy, but the tools have to be told where the proxy is or every
	// one of them just fails; the session environment comes last so an
	// operator override still wins.
	env := append(proxyEnvVars(resolved), p.buildEnv(opts)...)

	// Build resources
	resources := p.buildResources(opts)

	// Build container
	container := corev1.Container{
		Name:  "agent",
		Image: p.config.Image,
		Env:   env,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "workspace",
				MountPath: workspaceMountPath,
			},
		},
		Resources: resources,
	}

	if p.config.ImagePullPolicy != "" {
		container.ImagePullPolicy = corev1.PullPolicy(p.config.ImagePullPolicy)
	}

	if len(p.config.Cmd) > 0 {
		container.Command = p.config.Cmd
	}
	if len(p.config.Args) > 0 {
		container.Args = p.config.Args
	}

	// Build pod spec
	terminationGracePeriod := defaultTerminationGracePeriodSeconds
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   p.config.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{container},
			// Air-gapped pods have no DNS, so the server's address is pinned
			// into the pod's hosts file instead.
			HostAliases:                   controlPlaneHostAliases(resolved),
			RestartPolicy:                 corev1.RestartPolicy(p.config.RestartPolicy),
			TerminationGracePeriodSeconds: &terminationGracePeriod,
			Volumes: []corev1.Volume{
				{
					Name: "workspace",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
		},
	}

	// Add service account if configured
	if p.config.ServiceAccount != "" {
		pod.Spec.ServiceAccountName = p.config.ServiceAccount
	}

	// Add node selector if configured
	if len(p.config.NodeSelector) > 0 {
		pod.Spec.NodeSelector = p.config.NodeSelector
	}

	// Add tolerations if configured
	if len(p.config.Tolerations) > 0 {
		pod.Spec.Tolerations = p.buildTolerations()
	}

	// Add image pull secrets if configured
	if len(p.config.ImagePullSecrets) > 0 {
		for _, secret := range p.config.ImagePullSecrets {
			pod.Spec.ImagePullSecrets = append(pod.Spec.ImagePullSecrets,
				corev1.LocalObjectReference{Name: secret})
		}
	}

	return pod
}

func (p *Provider) buildEnv(opts provider.SpawnOptions) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "MARIONETTE_SERVER", Value: opts.ServerURL},
		{Name: "MARIONETTE_RUNNER_TOKEN", Value: opts.RunnerToken},
	}

	if opts.SandboxMode != "" {
		env = append(env, corev1.EnvVar{Name: "MARIONETTE_SANDBOX_MODE", Value: opts.SandboxMode})
	}

	for k, v := range opts.Environment {
		env = append(env, corev1.EnvVar{Name: k, Value: v})
	}

	return env
}

func (p *Provider) buildResources(opts provider.SpawnOptions) corev1.ResourceRequirements {
	resources := corev1.ResourceRequirements{
		Limits:   corev1.ResourceList{},
		Requests: corev1.ResourceList{},
	}

	// Memory
	memoryLimit := p.config.Resources.Memory
	if opts.MemoryMB > 0 {
		memoryLimit = fmt.Sprintf("%dMi", opts.MemoryMB)
	}
	resources.Limits[corev1.ResourceMemory] = resource.MustParse(memoryLimit)

	memoryRequest := memoryLimit
	if p.config.Resources.MemoryRequest != "" {
		memoryRequest = p.config.Resources.MemoryRequest
	}
	resources.Requests[corev1.ResourceMemory] = resource.MustParse(memoryRequest)

	// CPU
	cpuLimit := p.config.Resources.CPUs
	if opts.CPUs > 0 {
		cpuLimit = fmt.Sprintf("%dm", int(opts.CPUs*1000))
	}
	resources.Limits[corev1.ResourceCPU] = resource.MustParse(cpuLimit)

	cpuRequest := cpuLimit
	if p.config.Resources.CPURequest != "" {
		cpuRequest = p.config.Resources.CPURequest
	}
	resources.Requests[corev1.ResourceCPU] = resource.MustParse(cpuRequest)

	// Ephemeral storage
	if p.config.Resources.EphemeralStorage != "" {
		resources.Limits[corev1.ResourceEphemeralStorage] = resource.MustParse(p.config.Resources.EphemeralStorage)
	}

	return resources
}

func (p *Provider) buildTolerations() []corev1.Toleration {
	tolerations := make([]corev1.Toleration, 0, len(p.config.Tolerations))
	for _, t := range p.config.Tolerations {
		toleration := corev1.Toleration{
			Key:      t.Key,
			Operator: corev1.TolerationOperator(t.Operator),
			Value:    t.Value,
			Effect:   corev1.TaintEffect(t.Effect),
		}
		if t.TolerationSeconds != nil {
			toleration.TolerationSeconds = t.TolerationSeconds
		}
		tolerations = append(tolerations, toleration)
	}
	return tolerations
}

func (p *Provider) waitForPodReady(ctx context.Context, podName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for pod to be ready")
			}

			pod, err := p.client.GetPod(ctx, p.config.Namespace, podName)
			if err != nil {
				continue
			}

			switch pod.Status.Phase {
			case corev1.PodRunning:
				return nil
			case corev1.PodFailed, corev1.PodUnknown:
				reason := "unknown"
				if len(pod.Status.ContainerStatuses) > 0 {
					cs := pod.Status.ContainerStatuses[0]
					if cs.State.Terminated != nil {
						reason = cs.State.Terminated.Reason
					} else if cs.State.Waiting != nil {
						reason = cs.State.Waiting.Reason
					}
				}
				return fmt.Errorf("pod entered %s state: %s", pod.Status.Phase, reason)
			}
		}
	}
}

func (p *Provider) findPodNameByRunnerID(ctx context.Context, runnerID string) (string, error) {
	pods, err := p.client.ListPods(ctx, p.config.Namespace, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s/%s=%s", p.config.LabelPrefix, labelRunnerID, runnerID),
	})
	if err != nil {
		return "", fmt.Errorf("listing pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return "", &provider.ErrRunnerNotFound{RunnerID: runnerID}
	}

	return pods.Items[0].Name, nil
}

func (p *Provider) podToInstance(pod *corev1.Pod) *provider.RunnerInstance {
	runnerID := pod.Labels[p.labelKey(labelRunnerID)]

	return &provider.RunnerInstance{
		ID:         runnerID,
		ProviderID: string(pod.UID),
		Name:       pod.Name,
		Status:     mapPodPhase(pod.Status.Phase),
		CreatedAt:  pod.CreationTimestamp.Time,
		Labels:     pod.Labels,
		Metadata: map[string]string{
			"pod_name":   pod.Name,
			"pod_uid":    string(pod.UID),
			"namespace":  pod.Namespace,
			"node_name":  pod.Spec.NodeName,
			"cluster_ip": pod.Status.PodIP,
		},
	}
}

// mapPodPhase maps Kubernetes pod phase to InstanceStatus.
func mapPodPhase(phase corev1.PodPhase) provider.InstanceStatus {
	switch phase {
	case corev1.PodPending:
		return provider.InstanceStatusPending
	case corev1.PodRunning:
		return provider.InstanceStatusRunning
	case corev1.PodSucceeded:
		return provider.InstanceStatusStopped
	case corev1.PodFailed:
		return provider.InstanceStatusFailed
	case corev1.PodUnknown:
		return provider.InstanceStatusFailed
	default:
		return provider.InstanceStatusFailed
	}
}

// sanitizeName converts a string to a valid Kubernetes resource name.
func sanitizeName(s string) string {
	// Kubernetes names must be lowercase alphanumeric with hyphens
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "-")

	// Remove invalid characters
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}

	// Trim leading/trailing hyphens
	name := strings.Trim(result.String(), "-")

	// Truncate to max length (63 characters for K8s names)
	if len(name) > 63 {
		name = name[:63]
	}

	return name
}

// NewFromJSON creates a Kubernetes provider from raw JSON configuration.
func NewFromJSON(name string, configJSON, suspendConfigJSON json.RawMessage) (*Provider, error) {
	cfg, err := ParseConfig(configJSON)
	if err != nil {
		return nil, err
	}

	suspendCfg, err := provider.ParseSuspendConfig(suspendConfigJSON, defaultSuspendConfig())
	if err != nil {
		return nil, err
	}

	client, err := NewKubeClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	return &Provider{
		name:          name,
		config:        cfg,
		suspendConfig: suspendCfg,
		client:        client,
		resolver:      mnet.NewDNSResolver(),
	}, nil
}
