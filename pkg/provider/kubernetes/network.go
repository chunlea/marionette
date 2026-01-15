package kubernetes

import (
	"context"
	"fmt"
	"net"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/chunlea/marionette/pkg/provider"
)

// createNetworkPolicy creates a NetworkPolicy for a runner based on the network policy settings.
func (p *Provider) createNetworkPolicy(ctx context.Context, runnerID string, opts provider.SpawnOptions) error {
	np := p.buildNetworkPolicy(runnerID, opts)
	_, err := p.client.CreateNetworkPolicy(ctx, p.config.Namespace, np)
	if err != nil {
		return fmt.Errorf("creating network policy: %w", err)
	}
	return nil
}

// buildNetworkPolicy creates a NetworkPolicy based on the network policy settings.
func (p *Provider) buildNetworkPolicy(runnerID string, opts provider.SpawnOptions) *networkingv1.NetworkPolicy {
	npName := p.networkPolicyName(runnerID)

	labels := map[string]string{
		p.labelKey(labelManagedBy): "marionette",
		p.labelKey(labelRunnerID):  runnerID,
	}
	if opts.TenantID != "" {
		labels[p.labelKey(labelTenantID)] = opts.TenantID
	}

	// Selector to match the runner pod
	podSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			p.labelKey(labelRunnerID): runnerID,
		},
	}

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      npName,
			Namespace: p.config.Namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: podSelector,
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
		},
	}

	// Build egress rules based on network policy type
	switch opts.NetworkPolicy {
	case "allow_list":
		np.Spec.Egress = p.buildAllowListEgressRules(opts.AllowedHosts)
	case "proxy":
		// For proxy mode, allow only egress to the proxy server
		// This would need additional configuration for proxy address
		np.Spec.Egress = p.buildAllowListEgressRules(opts.AllowedHosts)
	case "air_gapped":
		// No egress allowed (empty egress rules = block all)
		np.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{}
	default:
		// "none" - allow all egress (don't create network policy)
		return nil
	}

	// Always allow DNS resolution (kube-dns/coredns)
	np.Spec.Egress = append(np.Spec.Egress, p.buildDNSEgressRule())

	return np
}

// buildAllowListEgressRules builds egress rules from allowed host patterns.
func (p *Provider) buildAllowListEgressRules(allowedHosts []string) []networkingv1.NetworkPolicyEgressRule {
	if len(allowedHosts) == 0 {
		return []networkingv1.NetworkPolicyEgressRule{}
	}

	var rules []networkingv1.NetworkPolicyEgressRule

	// Group hosts by whether they're IP addresses or hostnames
	var ipBlocks []*networkingv1.IPBlock
	var hostnameNotes []string

	for _, host := range allowedHosts {
		// Check if it's a CIDR or IP address
		if strings.Contains(host, "/") {
			// CIDR notation
			_, _, err := net.ParseCIDR(host)
			if err == nil {
				ipBlocks = append(ipBlocks, &networkingv1.IPBlock{
					CIDR: host,
				})
				continue
			}
		}

		// Check if it's a plain IP address
		if ip := net.ParseIP(host); ip != nil {
			cidr := host + "/32"
			if ip.To4() == nil {
				cidr = host + "/128"
			}
			ipBlocks = append(ipBlocks, &networkingv1.IPBlock{
				CIDR: cidr,
			})
			continue
		}

		// It's a hostname - we'll need to note that NetworkPolicy doesn't support hostnames
		// The actual DNS resolution and IP-based rules would need to be handled elsewhere
		// (e.g., by a sidecar proxy or external DNS policy controller)
		hostnameNotes = append(hostnameNotes, host)
	}

	// Create egress rule for IP blocks
	if len(ipBlocks) > 0 {
		peers := make([]networkingv1.NetworkPolicyPeer, 0, len(ipBlocks))
		for _, block := range ipBlocks {
			peers = append(peers, networkingv1.NetworkPolicyPeer{
				IPBlock: block,
			})
		}

		// Allow HTTP, HTTPS, and common development ports
		rules = append(rules, networkingv1.NetworkPolicyEgressRule{
			To:    peers,
			Ports: p.buildCommonPorts(),
		})
	}

	// For hostname-based rules, we allow all egress to those ports
	// since NetworkPolicy can't filter by hostname
	// A CNI with DNS-aware policies (like Cilium) would be needed for proper hostname filtering
	if len(hostnameNotes) > 0 {
		// Allow common ports to any destination
		// This is a fallback - proper hostname filtering requires CNI support
		rules = append(rules, networkingv1.NetworkPolicyEgressRule{
			Ports: p.buildCommonPorts(),
		})
	}

	return rules
}

// buildCommonPorts returns commonly needed ports for development.
func (p *Provider) buildCommonPorts() []networkingv1.NetworkPolicyPort {
	tcp := corev1.ProtocolTCP
	return []networkingv1.NetworkPolicyPort{
		{Protocol: &tcp, Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 80}},   // HTTP
		{Protocol: &tcp, Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 443}},  // HTTPS
		{Protocol: &tcp, Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 22}},   // SSH
		{Protocol: &tcp, Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 9418}}, // Git protocol
	}
}

// buildDNSEgressRule builds an egress rule allowing DNS resolution.
func (p *Provider) buildDNSEgressRule() networkingv1.NetworkPolicyEgressRule {
	tcp := corev1.ProtocolTCP
	udp := corev1.ProtocolUDP
	dnsPort := intstr.FromInt32(53)

	return networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{
				// Allow DNS to kube-system namespace (where CoreDNS typically runs)
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": "kube-system",
					},
				},
			},
		},
		Ports: []networkingv1.NetworkPolicyPort{
			{Protocol: &tcp, Port: &dnsPort},
			{Protocol: &udp, Port: &dnsPort},
		},
	}
}
