package cmd

import (
	"github.com/spf13/cobra"
	"github.com/svent/cliptun/channel"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "start network server",
	Long:  `start network server`,
	Run: func(cmd *cobra.Command, args []string) {
		options, err := getChannelOptions(cmd)
		if err != nil {
			errorLogger.Fatalln("cannot parse options:", err)
		}
		tunnel, err := channel.NewTunnel(channel.SERVER, options)
		if err != nil {
			errorLogger.Fatalln("Cannot create channel:", err)
		}

		tunnel.StartServer()

	},
}

func init() {
	rootCmd.AddCommand(serverCmd)
}
