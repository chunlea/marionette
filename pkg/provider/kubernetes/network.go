package kubernetes

import (
	"context"
	"fmt"
	"net"
	"sort"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	mnet "github.com/chunlea/marionette/pkg/network"
	"github.com/chunlea/marionette/pkg/provider"
)

// Network isolation on Kubernetes is a NetworkPolicy per runner.
//
// What this can do
//
//	Egress is default-deny once any egress rule exists, so a policy listing
//	only the control plane genuinely air-gaps a pod. Allow-list hosts are
//	resolved on the server and written as /32 or /128 ipBlocks, which is the
//	same resolve-and-pin approach the Docker provider uses and gives the same
//	protection against DNS rebinding.
//
// What it cannot do
//
//   - NetworkPolicy has no notion of a hostname. Wildcards such as
//     *.github.com cannot be expressed at all, and concrete names only work
//     because we resolve them first. A CNI with DNS-aware policy (Cilium's
//     toFQDNs, for instance) would be needed for real hostname filtering.
//   - Enforcement belongs to the CNI. On a cluster whose CNI ignores
//     NetworkPolicy (flannel without a policy add-on, for example) every
//     policy here is inert and the pod has full egress. Marionette cannot
//     detect that; the operator must.
//   - There is a window between a pod's sandbox being created and the CNI
//     programming its policy. Creating the policy before the pod, which is
//     what the provider does, shrinks it to whatever the CNI's own latency
//     is, but does not eliminate it.
//   - Proxy mode restricts egress to the proxy, but nothing here forces a
//     process to use it. The environment injection in buildPod does that, and
//     a tool that ignores the environment simply fails to reach anything.

// prepareNetworkPolicy turns spawn options plus operator config into a policy.
// It returns (nil, nil) when the session asks for no isolation.
func (p *Provider) prepareNetworkPolicy(opts provider.SpawnOptions) (*mnet.NetworkPolicy, error) {
	level := opts.NetworkPolicy
	if level == "" || level == string(mnet.PolicyNone) {
		return nil, nil
	}

	var policyOpts []mnet.PolicyOption

	serverAddr := opts.ServerURL
	if serverAddr == "" {
		serverAddr = p.config.Isolation.ServerURL
	}
	if serverAddr != "" {
		ep, err := mnet.ParseEndpoint(serverAddr, mnet.DefaultControlPlanePort)
		if err != nil {
			return nil, fmt.Errorf("parsing control plane address: %w", err)
		}
		policyOpts = append(policyOpts, mnet.WithControlPlane(ep))
	}

	if level == string(mnet.PolicyProxy) {
		proxy, err := mnet.ParseProxyConfig(p.config.Isolation.ProxyURL, p.config.Isolation.ProxyNoProxy, p.config.Isolation.ProxyCACert)
		if err != nil {
			return nil, fmt.Errorf("proxy policy requires a configured proxy: %w", err)
		}
		policyOpts = append(policyOpts, mnet.WithProxy(proxy))
	}

	policy, err := mnet.ParsePolicy(level, opts.AllowedHosts, policyOpts...)
	if err != nil {
		return nil, fmt.Errorf("parsing network policy: %w", err)
	}

	return policy, nil
}

// createNetworkPolicy resolves and installs the runner's NetworkPolicy.
func (p *Provider) createNetworkPolicy(ctx context.Context, runnerID string, opts provider.SpawnOptions, policy *mnet.NetworkPolicy) (*mnet.ResolvedPolicy, error) {
	resolved, err := p.resolver.ResolvePolicy(ctx, policy)
	if err != nil {
		return nil, fmt.Errorf("resolving network policy: %w", err)
	}

	np, err := p.buildNetworkPolicy(runnerID, opts, resolved)
	if err != nil {
		return nil, err
	}

	if _, err := p.client.CreateNetworkPolicy(ctx, p.config.Namespace, np); err != nil {
		return nil, fmt.Errorf("creating network policy: %w", err)
	}

	return resolved, nil
}

// buildNetworkPolicy renders a resolved policy as a Kubernetes NetworkPolicy.
//
// It returns (nil, nil) when no policy is required, and an error for an
// unrecognised level: silently returning nil sent a nil NetworkPolicy straight
// to the Kubernetes API.
func (p *Provider) buildNetworkPolicy(runnerID string, opts provider.SpawnOptions, resolved *mnet.ResolvedPolicy) (*networkingv1.NetworkPolicy, error) {
	if resolved == nil || resolved.OriginalPolicy == nil {
		return nil, nil
	}

	level := resolved.Level()
	if level == mnet.PolicyNone {
		return nil, nil
	}

	labels := map[string]string{
		p.labelKey(labelManagedBy): "marionette",
		p.labelKey(labelRunnerID):  runnerID,
	}
	if opts.TenantID != "" {
		labels[p.labelKey(labelTenantID)] = opts.TenantID
	}

	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.networkPolicyName(runnerID),
			Namespace: p.config.Namespace,
			Labels:    labels,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					p.labelKey(labelRunnerID): runnerID,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
			// An empty, non-nil egress list is the deny-all case. Leaving it
			// nil would mean "no egress rules declared", which Kubernetes reads
			// as no restriction at all.
			Egress: []networkingv1.NetworkPolicyEgressRule{},
		},
	}

	// The control plane is reachable at every level, air_gapped included.
	np.Spec.Egress = append(np.Spec.Egress, p.controlPlaneEgressRules(resolved)...)

	switch level {
	case mnet.PolicyAllowList:
		np.Spec.Egress = append(np.Spec.Egress, p.allowListEgressRules(resolved)...)
		np.Spec.Egress = append(np.Spec.Egress, p.dnsEgressRule())

	case mnet.PolicyProxy:
		// Only the proxy. Direct egress to the wider internet is not opened,
		// so a process that ignores the proxy environment fails closed.
		np.Spec.Egress = append(np.Spec.Egress, p.proxyEgressRules(resolved)...)
		np.Spec.Egress = append(np.Spec.Egress, p.dnsEgressRule())

	case mnet.PolicyAirGapped:
		// Nothing else, and deliberately no DNS: name resolution is a usable
		// exfiltration channel, and the server's address is injected into the
		// pod's hosts file instead.

	default:
		return nil, &provider.ErrInvalidConfig{
			Field:  "network_policy",
			Reason: fmt.Sprintf("unknown policy %q", level),
		}
	}

	return np, nil
}

// controlPlaneEgressRules opens the pinned server addresses.
func (p *Provider) controlPlaneEgressRules(resolved *mnet.ResolvedPolicy) []networkingv1.NetworkPolicyEgressRule {
	var rules []networkingv1.NetworkPolicyEgressRule

	for _, er := range resolved.ControlPlane {
		peers := ipBlockPeers(er.IPs)
		if len(peers) == 0 {
			continue
		}
		rules = append(rules, networkingv1.NetworkPolicyEgressRule{
			To:    peers,
			Ports: tcpPorts(er.Endpoint.Port),
		})
	}

	return rules
}

// proxyEgressRules opens the pinned proxy endpoint.
func (p *Provider) proxyEgressRules(resolved *mnet.ResolvedPolicy) []networkingv1.NetworkPolicyEgressRule {
	if resolved.Proxy == nil {
		return nil
	}

	peers := ipBlockPeers(resolved.Proxy.IPs)
	if len(peers) == 0 {
		return nil
	}

	return []networkingv1.NetworkPolicyEgressRule{{
		To:    peers,
		Ports: tcpPorts(resolved.Proxy.Endpoint.Port),
	}}
}

// allowListEgressRules opens the resolved allow-list addresses.
//
// Addresses are already filtered: the cloud metadata endpoint and the private
// ranges never reach here, however a hostile DNS answer resolves.
func (p *Provider) allowListEgressRules(resolved *mnet.ResolvedPolicy) []networkingv1.NetworkPolicyEgressRule {
	peers := ipBlockPeers(resolved.AllIPsFiltered())

	// Network blocks are expressed directly; they need no resolution and a
	// block overlapping a blocked range has already been dropped.
	for _, cidr := range resolved.AllowedCIDRs() {
		peers = append(peers, networkingv1.NetworkPolicyPeer{
			IPBlock: &networkingv1.IPBlock{CIDR: cidr.String()},
		})
	}

	if len(peers) == 0 {
		// No addresses resolved. An empty rule list is deny, which is the
		// correct outcome: an allow list nothing matched allows nothing.
		return nil
	}

	return []networkingv1.NetworkPolicyEgressRule{{
		To:    peers,
		Ports: tcpPorts(resolved.AllowedPorts...),
	}}
}

// dnsEgressRule allows resolution against the cluster DNS service.
func (p *Provider) dnsEgressRule() networkingv1.NetworkPolicyEgressRule {
	tcp := corev1.ProtocolTCP
	udp := corev1.ProtocolUDP
	dnsPort := intstr.FromInt32(53)

	namespace := p.config.Isolation.DNSNamespace
	if namespace == "" {
		namespace = DefaultDNSNamespace
	}

	return networkingv1.NetworkPolicyEgressRule{
		To: []networkingv1.NetworkPolicyPeer{
			{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": namespace,
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

// ipBlockPeers renders addresses as single-host ipBlocks.
func ipBlockPeers(ips []net.IP) []networkingv1.NetworkPolicyPeer {
	peers := make([]networkingv1.NetworkPolicyPeer, 0, len(ips))
	for _, ip := range ips {
		suffix := "/32"
		if ip.To4() == nil {
			suffix = "/128"
		}
		peers = append(peers, networkingv1.NetworkPolicyPeer{
			IPBlock: &networkingv1.IPBlock{CIDR: ip.String() + suffix},
		})
	}
	return peers
}

// tcpPorts renders TCP port matchers.
func tcpPorts(ports ...int) []networkingv1.NetworkPolicyPort {
	tcp := corev1.ProtocolTCP
	out := make([]networkingv1.NetworkPolicyPort, 0, len(ports))
	for _, port := range ports {
		value := intstr.FromInt32(int32(port))
		out = append(out, networkingv1.NetworkPolicyPort{Protocol: &tcp, Port: &value})
	}
	return out
}

// controlPlaneHostAliases pins the server's name to its address in the pod's
// hosts file.
//
// Air-gapped pods have no DNS, so this is the only way the agent can turn the
// server's hostname into an address.
func controlPlaneHostAliases(resolved *mnet.ResolvedPolicy) []corev1.HostAlias {
	if resolved == nil {
		return nil
	}

	var aliases []corev1.HostAlias
	for _, er := range resolved.ControlPlane {
		if er.Endpoint.IsIP() || len(er.IPs) == 0 {
			continue
		}
		aliases = append(aliases, corev1.HostAlias{
			IP:        er.IPs[0].String(),
			Hostnames: []string{er.Endpoint.Host},
		})
	}
	return aliases
}

// proxyEnvVars renders the proxy environment for a resolved policy.
func proxyEnvVars(resolved *mnet.ResolvedPolicy) []corev1.EnvVar {
	if resolved == nil || resolved.OriginalPolicy == nil || resolved.OriginalPolicy.Proxy == nil {
		return nil
	}

	noProxy := make([]string, 0, len(resolved.ControlPlane))
	for _, er := range resolved.ControlPlane {
		noProxy = append(noProxy, er.Endpoint.Host)
	}

	env := resolved.OriginalPolicy.Proxy.Env(noProxy...)

	// Sorted so two spawns of the same session produce identical pod specs,
	// which is what keeps a diff against a live object meaningful.
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	vars := make([]corev1.EnvVar, 0, len(keys))
	for _, k := range keys {
		vars = append(vars, corev1.EnvVar{Name: k, Value: env[k]})
	}
	return vars
}
