package main

import (
	"fmt"

	"github.com/chunlea/marionette/pkg/client"
	"github.com/spf13/cobra"
)

// tunnelsCreateFlags holds flags for the tunnels create command.
var tunnelsCreateFlags struct {
	sessionID  string
	tunnelType string
	localPort  int
	public     bool
}

var tunnelsCmd = &cobra.Command{
	Use:     "tunnels",
	Aliases: []string{"tunnel", "tun"},
	Short:   "Manage tunnels",
	Long:    `Manage Marionette tunnels - expose runner ports to the network.`,
}

func init() {
	tunnelsCmd.AddCommand(tunnelsCreateCmd)
	tunnelsCmd.AddCommand(tunnelsListCmd)
	tunnelsCmd.AddCommand(tunnelsGetCmd)
	tunnelsCmd.AddCommand(tunnelsCloseCmd)

	// Flags for tunnels create
	tunnelsCreateCmd.Flags().StringVar(&tunnelsCreateFlags.sessionID, "session", "", "session ID (required)")
	tunnelsCreateCmd.Flags().StringVar(&tunnelsCreateFlags.tunnelType, "type", "http", "tunnel type (http, tcp)")
	tunnelsCreateCmd.Flags().IntVar(&tunnelsCreateFlags.localPort, "port", 0, "local port to tunnel (required)")
	tunnelsCreateCmd.Flags().BoolVar(&tunnelsCreateFlags.public, "public", false, "make tunnel publicly accessible (no authentication required)")
	_ = tunnelsCreateCmd.MarkFlagRequired("session")
	_ = tunnelsCreateCmd.MarkFlagRequired("port")
}

var tunnelsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new tunnel",
	Long: `Create a new tunnel to expose a port on the runner.

The server will send a request to the runner to establish the tunnel.
Once created, the tunnel can be accessed via its public URL.

By default, tunnels require authentication via:
  - X-Marionette-Tunnel-Token header
  - marionette_token query parameter
  - HTTP Basic Auth (password = tunnel token)

Use --public to create a tunnel without authentication.

Examples:
  # Create an HTTP tunnel for port 8000
  mctl tunnels create --session sess_xxx --port 8000

  # Create a public tunnel (no authentication required)
  mctl tunnels create --session sess_xxx --port 8000 --public

  # Create a TCP tunnel
  mctl tunnels create --session sess_xxx --port 5432 --type tcp`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		opts := client.CreateTunnelOptions{
			SessionID: tunnelsCreateFlags.sessionID,
			Type:      tunnelsCreateFlags.tunnelType,
			LocalPort: tunnelsCreateFlags.localPort,
			Public:    tunnelsCreateFlags.public,
		}

		tunnel, err := apiClient.CreateTunnel(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to create tunnel: %w", err)
		}

		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintTunnel(tunnel)
	},
}

var tunnelsListCmd = &cobra.Command{
	Use:   "list SESSION_ID",
	Short: "List tunnels for a session",
	Long: `List all active tunnels for a session.

Example:
  mctl tunnels list sess_BxKmNpVq1StGXR8a`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		sessionID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		opts := client.ListTunnelsOptions{
			SessionID: sessionID,
		}

		result, err := apiClient.ListTunnels(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to list tunnels: %w", err)
		}

		if len(result.Items) == 0 {
			printf("No tunnels found.\n")
			return nil
		}

		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintTunnelList(result.Items)
	},
}

var tunnelsGetCmd = &cobra.Command{
	Use:   "get TUNNEL_ID",
	Short: "Get tunnel details",
	Long: `Get detailed information about a specific tunnel.

Example:
  mctl tunnels get tun_BxKmNpVq1StGXR8a`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		tunnelID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		tunnel, err := apiClient.GetTunnel(ctx, tunnelID)
		if err != nil {
			if client.IsNotFound(err) {
				return fmt.Errorf("tunnel %q not found", tunnelID)
			}
			return fmt.Errorf("failed to get tunnel: %w", err)
		}

		printer := NewPrinter(outputFmt, getOutput())
		return printer.PrintTunnel(tunnel)
	},
}

var tunnelsCloseCmd = &cobra.Command{
	Use:   "close TUNNEL_ID",
	Short: "Close a tunnel",
	Long: `Close an active tunnel.

Example:
  mctl tunnels close tun_BxKmNpVq1StGXR8a`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		tunnelID := args[0]

		if apiClient == nil {
			return fmt.Errorf("no API client configured. Use --server and --api-key or configure a context")
		}

		if err := apiClient.CloseTunnel(ctx, tunnelID); err != nil {
			if client.IsNotFound(err) {
				return fmt.Errorf("tunnel %q not found", tunnelID)
			}
			return fmt.Errorf("failed to close tunnel: %w", err)
		}

		printf("Tunnel %s closed.\n", tunnelID)
		return nil
	},
}
