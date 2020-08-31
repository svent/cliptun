package cmd

import (
	"io"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/svent/cliptun/channel"
	"github.com/svent/go-nbreader"
)

var execCmd = &cobra.Command{
	Use:   "exec",
	Short: "execute a command",
	Long:  `execute a command and connect STDIN/STDOUT`,
	Run: func(cmd *cobra.Command, args []string) {
		options, err := getChannelOptions(cmd)
		if err != nil {
			errorLogger.Fatalln("cannot parse options:", err)
		}
		channel, err := channel.NewChannel(channel.SERVER, options)
		if err != nil {
			errorLogger.Fatalln("Cannot create channel:", err)
		}

		if len(args) == 0 || len(args[0]) == 0 {
			errorLogger.Fatalln("no command given")
		}
		commandline := args[0]
		command := exec.Command(commandline)
		stdin, err := command.StdinPipe()
		if err != nil {
			errorLogger.Fatalln("cannot access stdin of command:", err)
		}
		stdout, err := command.StdoutPipe()
		if err != nil {
			errorLogger.Fatalln("cannot access stdout of command:", err)
		}
		stdoutNB := nbreader.NewNBReader(stdout, options.Blocksize, nbreader.Timeout(options.Interval*4/5))
		command.Start()

		var commandExited = make(chan error)
		go func() {
			err := command.Wait()
			commandExited <- err
		}()

		for {
			select {
			case cmdErr := <-commandExited:
				if err == nil {
					debugLogger.Printf("Program '%s' terminated.\n", commandline)
				} else {
					debugLogger.Printf("Program '%s' terminated with error: %s\n", commandline, cmdErr)
				}
				channel.CloseChannel()
			default:
			}
			cbdata := channel.Receive()
			if len(cbdata) == 0 {
				continue
			}
			debugLogger.Println("got data:", string(cbdata))
			stdin.Write(cbdata)
			data := make([]byte, options.Blocksize)
			length, err := stdoutNB.Read(data)
			if err == io.EOF {
				debugLogger.Printf("Program '%s' terminated.\n", commandline)
				channel.CloseChannel()
			}
			channel.Send(data[0:length])
		}
	},
}

func init() {
	rootCmd.AddCommand(execCmd)
}
