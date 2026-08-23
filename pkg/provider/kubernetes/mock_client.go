package kubernetes

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

// MockKubeClient is a mock implementation of KubeClient for testing.
type MockKubeClient struct {
	mu sync.RWMutex

	// Storage
	pods            map[string]*corev1.Pod                   // namespace/name -> pod
	pvcs            map[string]*corev1.PersistentVolumeClaim // namespace/name -> pvc
	networkPolicies map[string]*networkingv1.NetworkPolicy   // namespace/name -> policy
	namespaces      map[string]*corev1.Namespace             // name -> namespace

	// Error injection
	CreatePodErr           error
	GetPodErr              error
	DeletePodErr           error
	ListPodsErr            error
	CreatePVCErr           error
	GetPVCErr              error
	DeletePVCErr           error
	CreateNetworkPolicyErr error
	GetNetworkPolicyErr    error
	DeleteNetworkPolicyErr error
	GetNamespaceErr        error

	// Call tracking
	CreatePodCalls           []createPodCall
	DeletePodCalls           []deletePodCall
	CreatePVCCalls           []createPVCCall
	DeletePVCCalls           []deletePVCCall
	CreateNetworkPolicyCalls []createNetworkPolicyCall
	DeleteNetworkPolicyCalls []deleteNetworkPolicyCall

	// seq is a monotonic counter stamped on recorded calls, so a test can
	// assert that one resource was created before another.
	seq int
}

type createPodCall struct {
	Namespace string
	Pod       *corev1.Pod
	Seq       int
}

type deletePodCall struct {
	Namespace string
	Name      string
}

type createPVCCall struct {
	Namespace string
	PVC       *corev1.PersistentVolumeClaim
}

type deletePVCCall struct {
	Namespace string
	Name      string
}

type createNetworkPolicyCall struct {
	Namespace string
	Policy    *networkingv1.NetworkPolicy
	Seq       int
}

type deleteNetworkPolicyCall struct {
	Namespace string
	Name      string
}

// NewMockKubeClient creates a new mock client.
func NewMockKubeClient() *MockKubeClient {
	return &MockKubeClient{
		pods:            make(map[string]*corev1.Pod),
		pvcs:            make(map[string]*corev1.PersistentVolumeClaim),
		networkPolicies: make(map[string]*networkingv1.NetworkPolicy),
		namespaces:      make(map[string]*corev1.Namespace),
	}
}

// Ensure MockKubeClient implements KubeClient.
var _ KubeClient = (*MockKubeClient)(nil)

func (m *MockKubeClient) key(namespace, name string) string {
	return namespace + "/" + name
}

// nextSeq returns the next call ordinal. Caller holds the write lock.
func (m *MockKubeClient) nextSeq() int {
	m.seq++
	return m.seq
}

// AddNamespace adds a namespace to the mock.
func (m *MockKubeClient) AddNamespace(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.namespaces[name] = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

// AddPod adds a pod to the mock.
func (m *MockKubeClient) AddPod(pod *corev1.Pod) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pods[m.key(pod.Namespace, pod.Name)] = pod
}

// AddPVC adds a PVC to the mock.
func (m *MockKubeClient) AddPVC(pvc *corev1.PersistentVolumeClaim) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pvcs[m.key(pvc.Namespace, pvc.Name)] = pvc
}

// GetStoredPod returns a stored pod (copy for safe concurrent access).
func (m *MockKubeClient) GetStoredPod(namespace, name string) *corev1.Pod {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pod := m.pods[m.key(namespace, name)]
	if pod == nil {
		return nil
	}
	return pod.DeepCopy()
}

// UpdatePodStatus atomically updates a pod's status.
func (m *MockKubeClient) UpdatePodStatus(namespace, name string, updateFn func(*corev1.Pod)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pod := m.pods[m.key(namespace, name)]; pod != nil {
		updateFn(pod)
	}
}

// GetStoredPVC returns a stored PVC.
func (m *MockKubeClient) GetStoredPVC(namespace, name string) *corev1.PersistentVolumeClaim {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pvcs[m.key(namespace, name)]
}

// GetStoredNetworkPolicy returns a stored network policy.
func (m *MockKubeClient) GetStoredNetworkPolicy(namespace, name string) *networkingv1.NetworkPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.networkPolicies[m.key(namespace, name)]
}

// Pod operations

func (m *MockKubeClient) CreatePod(ctx context.Context, namespace string, pod *corev1.Pod) (*corev1.Pod, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CreatePodCalls = append(m.CreatePodCalls, createPodCall{Namespace: namespace, Pod: pod, Seq: m.nextSeq()})

	if m.CreatePodErr != nil {
		return nil, m.CreatePodErr
	}

	key := m.key(namespace, pod.Name)
	if _, exists := m.pods[key]; exists {
		return nil, k8serrors.NewAlreadyExists(schema.GroupResource{Resource: "pods"}, pod.Name)
	}

	// Set defaults
	pod.Namespace = namespace
	if pod.UID == "" {
		pod.UID = "mock-uid-" + types.UID(pod.Name)
	}
	if pod.Status.Phase == "" {
		pod.Status.Phase = corev1.PodPending
	}
	pod.CreationTimestamp = metav1.Now()

	// Store a copy
	stored := pod.DeepCopy()
	m.pods[key] = stored

	return stored.DeepCopy(), nil
}

func (m *MockKubeClient) GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.GetPodErr != nil {
		return nil, m.GetPodErr
	}

	pod, exists := m.pods[m.key(namespace, name)]
	if !exists {
		return nil, k8serrors.NewNotFound(schema.GroupResource{Resource: "pods"}, name)
	}

	return pod.DeepCopy(), nil
}

func (m *MockKubeClient) DeletePod(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.DeletePodCalls = append(m.DeletePodCalls, deletePodCall{Namespace: namespace, Name: name})

	if m.DeletePodErr != nil {
		return m.DeletePodErr
	}

	key := m.key(namespace, name)
	if _, exists := m.pods[key]; !exists {
		return k8serrors.NewNotFound(schema.GroupResource{Resource: "pods"}, name)
	}

	delete(m.pods, key)
	return nil
}

func (m *MockKubeClient) ListPods(ctx context.Context, namespace string, opts metav1.ListOptions) (*corev1.PodList, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.ListPodsErr != nil {
		return nil, m.ListPodsErr
	}

	list := &corev1.PodList{}
	for key, pod := range m.pods {
		if namespace != "" && pod.Namespace != namespace {
			continue
		}

		// Simple label selector matching
		if opts.LabelSelector != "" {
			if !matchesLabelSelector(pod.Labels, opts.LabelSelector) {
				continue
			}
		}

		_ = key // avoid unused warning
		list.Items = append(list.Items, *pod.DeepCopy())
	}

	return list, nil
}

func (m *MockKubeClient) WatchPods(ctx context.Context, namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	// Not implemented for tests
	return nil, fmt.Errorf("watch not implemented")
}

// PVC operations

func (m *MockKubeClient) CreatePVC(ctx context.Context, namespace string, pvc *corev1.PersistentVolumeClaim) (*corev1.PersistentVolumeClaim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CreatePVCCalls = append(m.CreatePVCCalls, createPVCCall{Namespace: namespace, PVC: pvc})

	if m.CreatePVCErr != nil {
		return nil, m.CreatePVCErr
	}

	key := m.key(namespace, pvc.Name)
	if _, exists := m.pvcs[key]; exists {
		return nil, k8serrors.NewAlreadyExists(schema.GroupResource{Resource: "persistentvolumeclaims"}, pvc.Name)
	}

	pvc.Namespace = namespace
	pvc.CreationTimestamp = metav1.Now()

	stored := pvc.DeepCopy()
	m.pvcs[key] = stored

	return stored.DeepCopy(), nil
}

func (m *MockKubeClient) GetPVC(ctx context.Context, namespace, name string) (*corev1.PersistentVolumeClaim, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.GetPVCErr != nil {
		return nil, m.GetPVCErr
	}

	pvc, exists := m.pvcs[m.key(namespace, name)]
	if !exists {
		return nil, k8serrors.NewNotFound(schema.GroupResource{Resource: "persistentvolumeclaims"}, name)
	}

	return pvc.DeepCopy(), nil
}

func (m *MockKubeClient) DeletePVC(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.DeletePVCCalls = append(m.DeletePVCCalls, deletePVCCall{Namespace: namespace, Name: name})

	if m.DeletePVCErr != nil {
		return m.DeletePVCErr
	}

	key := m.key(namespace, name)
	if _, exists := m.pvcs[key]; !exists {
		return k8serrors.NewNotFound(schema.GroupResource{Resource: "persistentvolumeclaims"}, name)
	}

	delete(m.pvcs, key)
	return nil
}

func (m *MockKubeClient) ListPVCs(ctx context.Context, namespace string, opts metav1.ListOptions) (*corev1.PersistentVolumeClaimList, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := &corev1.PersistentVolumeClaimList{}
	for _, pvc := range m.pvcs {
		if namespace != "" && pvc.Namespace != namespace {
			continue
		}
		list.Items = append(list.Items, *pvc.DeepCopy())
	}

	return list, nil
}

// NetworkPolicy operations

func (m *MockKubeClient) CreateNetworkPolicy(ctx context.Context, namespace string, np *networkingv1.NetworkPolicy) (*networkingv1.NetworkPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.CreateNetworkPolicyCalls = append(m.CreateNetworkPolicyCalls, createNetworkPolicyCall{Namespace: namespace, Policy: np, Seq: m.nextSeq()})

	if m.CreateNetworkPolicyErr != nil {
		return nil, m.CreateNetworkPolicyErr
	}

	key := m.key(namespace, np.Name)
	if _, exists := m.networkPolicies[key]; exists {
		return nil, k8serrors.NewAlreadyExists(schema.GroupResource{Resource: "networkpolicies"}, np.Name)
	}

	np.Namespace = namespace
	np.CreationTimestamp = metav1.Now()

	stored := np.DeepCopy()
	m.networkPolicies[key] = stored

	return stored.DeepCopy(), nil
}

func (m *MockKubeClient) GetNetworkPolicy(ctx context.Context, namespace, name string) (*networkingv1.NetworkPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.GetNetworkPolicyErr != nil {
		return nil, m.GetNetworkPolicyErr
	}

	np, exists := m.networkPolicies[m.key(namespace, name)]
	if !exists {
		return nil, k8serrors.NewNotFound(schema.GroupResource{Resource: "networkpolicies"}, name)
	}

	return np.DeepCopy(), nil
}

func (m *MockKubeClient) DeleteNetworkPolicy(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.DeleteNetworkPolicyCalls = append(m.DeleteNetworkPolicyCalls, deleteNetworkPolicyCall{Namespace: namespace, Name: name})

	if m.DeleteNetworkPolicyErr != nil {
		return m.DeleteNetworkPolicyErr
	}

	key := m.key(namespace, name)
	if _, exists := m.networkPolicies[key]; !exists {
		return k8serrors.NewNotFound(schema.GroupResource{Resource: "networkpolicies"}, name)
	}

	delete(m.networkPolicies, key)
	return nil
}

func (m *MockKubeClient) ListNetworkPolicies(ctx context.Context, namespace string, opts metav1.ListOptions) (*networkingv1.NetworkPolicyList, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := &networkingv1.NetworkPolicyList{}
	for _, np := range m.networkPolicies {
		if namespace != "" && np.Namespace != namespace {
			continue
		}
		list.Items = append(list.Items, *np.DeepCopy())
	}

	return list, nil
}

// Namespace operations

func (m *MockKubeClient) GetNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.GetNamespaceErr != nil {
		return nil, m.GetNamespaceErr
	}

	ns, exists := m.namespaces[name]
	if !exists {
		return nil, k8serrors.NewNotFound(schema.GroupResource{Resource: "namespaces"}, name)
	}

	return ns.DeepCopy(), nil
}

// SetPodPhase sets the phase of a stored pod.
func (m *MockKubeClient) SetPodPhase(namespace, name string, phase corev1.PodPhase) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if pod, exists := m.pods[m.key(namespace, name)]; exists {
		pod.Status.Phase = phase
	}
}

// matchesLabelSelector performs a simple label selector matching.
// This is a simplified implementation that handles basic selectors like "key=value".
func matchesLabelSelector(labels map[string]string, selector string) bool {
	if selector == "" {
		return true
	}

	// Parse selector - simple format: "key1=value1,key2=value2"
	parts := split(selector, ",")
	for _, part := range parts {
		if kv := split(part, "="); len(kv) == 2 {
			key := kv[0]
			value := kv[1]
			if labels[key] != value {
				return false
			}
		}
	}

	return true
}

// split is a helper to split strings.
func split(s, sep string) []string {
	if s == "" {
		return nil
	}
	var result []string
	for len(s) > 0 {
		idx := indexOf(s, sep)
		if idx == -1 {
			result = append(result, s)
			break
		}
		result = append(result, s[:idx])
		s = s[idx+len(sep):]
	}
	return result
}

func indexOf(s, sep string) int {
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			return i
		}
	}
	return -1
}
