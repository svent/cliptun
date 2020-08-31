package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/svent/cliptun/channel"
	"github.com/svent/cliptun/shell"
)

var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "start network client",
	Long:  `start network client allowing to initiate port forwardings`,
	Run: func(cmd *cobra.Command, args []string) {
		options, err := getChannelOptions(cmd)
		if err != nil {
			errorLogger.Fatalln("cannot parse options:", err)
		}
		tunnel, err := channel.NewTunnel(channel.CLIENT, options)
		if err != nil {
			errorLogger.Fatalln("Cannot create channel:", err)
		}

		var localPortForwardings []channel.PortForwarding
		var remotePortForwardings []channel.PortForwarding

		fl, _ := cmd.Flags().GetStringSlice("fwd-local")
		for _, param := range fl {
			portfwd, err := channel.ParsePortForwarding(param)
			if err != nil {
				errorLogger.Fatalln(err)
			}
			localPortForwardings = append(localPortForwardings, *portfwd)
		}

		fr, _ := cmd.Flags().GetStringSlice("fwd-remote")
		for _, param := range fr {
			portfwd, err := channel.ParsePortForwarding(param)
			if err != nil {
				errorLogger.Fatalln(err)
			}
			remotePortForwardings = append(remotePortForwardings, *portfwd)
		}

		tunnel.StartClient()

		socksPort, _ := cmd.Flags().GetInt("socks")
		if socksPort > 0 {
			tunnel.StartSocksOnPort(socksPort)
		}

		for _, fwd := range localPortForwardings {
			tunnel.AddLocalPortForwarding(fwd)
		}
		for _, fwd := range remotePortForwardings {
			tunnel.AddRemotePortForwarding(fwd)
		}

		fmt.Println("Connected, type 'help' for a list of commands.")
		shell.Execute(tunnel)
	},
}

func init() {
	clientCmd.Flags().StringSlice("fwd-local", []string{}, "forward local port to remote host and port (LPORT:RHOST:RPORT)")
	clientCmd.Flags().StringSlice("fwd-remote", []string{}, "forward remote port to local host and port (RPORT:LHOST:LPORT)")
	clientCmd.Flags().Int("socks", 0, "start SOCKS5 server on the given port")
	rootCmd.AddCommand(clientCmd)
}
