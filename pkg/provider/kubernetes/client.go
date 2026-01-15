package kubernetes

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubeClient abstracts Kubernetes API operations for testing.
type KubeClient interface {
	// Pod operations
	CreatePod(ctx context.Context, namespace string, pod *corev1.Pod) (*corev1.Pod, error)
	GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error)
	DeletePod(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error
	ListPods(ctx context.Context, namespace string, opts metav1.ListOptions) (*corev1.PodList, error)
	WatchPods(ctx context.Context, namespace string, opts metav1.ListOptions) (watch.Interface, error)

	// PVC operations
	CreatePVC(ctx context.Context, namespace string, pvc *corev1.PersistentVolumeClaim) (*corev1.PersistentVolumeClaim, error)
	GetPVC(ctx context.Context, namespace, name string) (*corev1.PersistentVolumeClaim, error)
	DeletePVC(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error
	ListPVCs(ctx context.Context, namespace string, opts metav1.ListOptions) (*corev1.PersistentVolumeClaimList, error)

	// NetworkPolicy operations
	CreateNetworkPolicy(ctx context.Context, namespace string, np *networkingv1.NetworkPolicy) (*networkingv1.NetworkPolicy, error)
	GetNetworkPolicy(ctx context.Context, namespace, name string) (*networkingv1.NetworkPolicy, error)
	DeleteNetworkPolicy(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error
	ListNetworkPolicies(ctx context.Context, namespace string, opts metav1.ListOptions) (*networkingv1.NetworkPolicyList, error)

	// Namespace operations
	GetNamespace(ctx context.Context, name string) (*corev1.Namespace, error)
}

// kubeClientWrapper wraps the official Kubernetes clientset to implement KubeClient.
type kubeClientWrapper struct {
	clientset *kubernetes.Clientset
}

// NewKubeClient creates a Kubernetes client from config.
func NewKubeClient(cfg *Config) (KubeClient, error) {
	var restConfig *rest.Config
	var err error

	if cfg.Kubeconfig != "" {
		// Load from explicit kubeconfig file
		loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.Kubeconfig}
		configOverrides := &clientcmd.ConfigOverrides{}
		if cfg.Context != "" {
			configOverrides.CurrentContext = cfg.Context
		}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		restConfig, err = kubeConfig.ClientConfig()
	} else {
		// Try in-cluster config first
		restConfig, err = rest.InClusterConfig()
		if err != nil {
			// Fall back to default kubeconfig (e.g., ~/.kube/config)
			loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
			configOverrides := &clientcmd.ConfigOverrides{}
			if cfg.Context != "" {
				configOverrides.CurrentContext = cfg.Context
			}
			kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
			restConfig, err = kubeConfig.ClientConfig()
		}
	}

	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	return &kubeClientWrapper{clientset: clientset}, nil
}

// NewKubeClientFromClientset creates a KubeClient from an existing clientset (for testing).
func NewKubeClientFromClientset(clientset *kubernetes.Clientset) KubeClient {
	return &kubeClientWrapper{clientset: clientset}
}

// Ensure kubeClientWrapper implements KubeClient.
var _ KubeClient = (*kubeClientWrapper)(nil)

// Pod operations

func (k *kubeClientWrapper) CreatePod(ctx context.Context, namespace string, pod *corev1.Pod) (*corev1.Pod, error) {
	return k.clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
}

func (k *kubeClientWrapper) GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error) {
	return k.clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (k *kubeClientWrapper) DeletePod(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error {
	return k.clientset.CoreV1().Pods(namespace).Delete(ctx, name, opts)
}

func (k *kubeClientWrapper) ListPods(ctx context.Context, namespace string, opts metav1.ListOptions) (*corev1.PodList, error) {
	return k.clientset.CoreV1().Pods(namespace).List(ctx, opts)
}

func (k *kubeClientWrapper) WatchPods(ctx context.Context, namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	return k.clientset.CoreV1().Pods(namespace).Watch(ctx, opts)
}

// PVC operations

func (k *kubeClientWrapper) CreatePVC(ctx context.Context, namespace string, pvc *corev1.PersistentVolumeClaim) (*corev1.PersistentVolumeClaim, error) {
	return k.clientset.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, pvc, metav1.CreateOptions{})
}

func (k *kubeClientWrapper) GetPVC(ctx context.Context, namespace, name string) (*corev1.PersistentVolumeClaim, error) {
	return k.clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (k *kubeClientWrapper) DeletePVC(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error {
	return k.clientset.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, name, opts)
}

func (k *kubeClientWrapper) ListPVCs(ctx context.Context, namespace string, opts metav1.ListOptions) (*corev1.PersistentVolumeClaimList, error) {
	return k.clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, opts)
}

// NetworkPolicy operations

func (k *kubeClientWrapper) CreateNetworkPolicy(ctx context.Context, namespace string, np *networkingv1.NetworkPolicy) (*networkingv1.NetworkPolicy, error) {
	return k.clientset.NetworkingV1().NetworkPolicies(namespace).Create(ctx, np, metav1.CreateOptions{})
}

func (k *kubeClientWrapper) GetNetworkPolicy(ctx context.Context, namespace, name string) (*networkingv1.NetworkPolicy, error) {
	return k.clientset.NetworkingV1().NetworkPolicies(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (k *kubeClientWrapper) DeleteNetworkPolicy(ctx context.Context, namespace, name string, opts metav1.DeleteOptions) error {
	return k.clientset.NetworkingV1().NetworkPolicies(namespace).Delete(ctx, name, opts)
}

func (k *kubeClientWrapper) ListNetworkPolicies(ctx context.Context, namespace string, opts metav1.ListOptions) (*networkingv1.NetworkPolicyList, error) {
	return k.clientset.NetworkingV1().NetworkPolicies(namespace).List(ctx, opts)
}

// Namespace operations

func (k *kubeClientWrapper) GetNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	return k.clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
}
