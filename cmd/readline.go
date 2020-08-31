package cmd

import (
	"bufio"
	"os"

	"github.com/spf13/cobra"
	"github.com/svent/cliptun/channel"
)

var readlineCmd = &cobra.Command{
	Use:   "readline",
	Short: "read from STDIN",
	Long:  `read from STDIN (line by line)`,
	Run: func(cmd *cobra.Command, args []string) {
		options, err := getChannelOptions(cmd)
		if err != nil {
			errorLogger.Fatalln("cannot parse options:", err)
		}
		channel, err := channel.NewChannel(channel.CLIENT, options)
		if err != nil {
			errorLogger.Fatalln("Cannot create channel:", err)
		}

		lineChan := make(chan string)
		go func(lineChan chan string) {
			scanner := bufio.NewScanner(os.Stdin)
			for scanner.Scan() {
				line := scanner.Text()
				line += "\n"
				lineChan <- line
			}
		}(lineChan)

		for {
			var line string
			select {
			case line = <-lineChan:
				channel.Send([]byte(line))
			default:
				channel.Send([]byte{})
			}
			cbdata := channel.Receive()
			os.Stdout.Write([]byte(cbdata))
		}
	},
}

func init() {
	rootCmd.AddCommand(readlineCmd)
}
