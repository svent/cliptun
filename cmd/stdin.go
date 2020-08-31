package cmd

import (
	"os"
	"sync"

	"github.com/spf13/cobra"
	"github.com/svent/cliptun/channel"
)

var stdinCmd = &cobra.Command{
	Use:   "stdin",
	Short: "read from STDIN",
	Long:  `read all data from STDIN`,
	Run: func(cmd *cobra.Command, args []string) {
		options, err := getChannelOptions(cmd)
		if err != nil {
			errorLogger.Fatalln("cannot parse options:", err)
		}
		channel, err := channel.NewChannel(channel.CLIENT, options)
		if err != nil {
			errorLogger.Fatalln("cannot create channel:", err)
		}

		var sendFIN sync.Once
		for {
			data := make([]byte, options.Blocksize)
			length, err := os.Stdin.Read(data)
			// if err == io.EOF {
			if err != nil {
				sendFIN.Do(func() {
					channel.CloseChannel()
				})
			} else {
				channel.Send(data[0:length])
			}
			channel.Receive()
		}
	},
}

func init() {
	rootCmd.AddCommand(stdinCmd)
}
