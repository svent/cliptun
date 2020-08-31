package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/svent/cliptun/channel"
)

var stdoutCmd = &cobra.Command{
	Use:   "stdout",
	Short: "write to STDOUT",
	Long:  `write all received data to STDOUT`,
	Run: func(cmd *cobra.Command, args []string) {
		options, err := getChannelOptions(cmd)
		if err != nil {
			errorLogger.Fatalln("cannot parse options:", err)
		}
		channel, err := channel.NewChannel(channel.SERVER, options)
		if err != nil {
			errorLogger.Fatalln("Cannot create channel:", err)
		}

		for {
			cbdata := channel.Receive()
			os.Stdout.Write(cbdata)
			data := []byte("")
			channel.Send(data)
		}
	},
}

func init() {
	rootCmd.AddCommand(stdoutCmd)
}
