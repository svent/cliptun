package cmd

import (
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var (
	errorLogger = log.New(os.Stderr, "Error: ", 0)
	debugLogger = log.New(ioutil.Discard, "", 0)
	traceLogger = log.New(ioutil.Discard, "", 0)

	rootCmd = &cobra.Command{
		Use:           "cliptun",
		Short:         "cliptun: create a tunnel using a synchronized clipboard",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			debug, err := cmd.Flags().GetBool("debug")
			if err != nil {
				errorLogger.Fatalln("cannot parse debug flag:", err)
			}
			if debug {
				debugLogger = log.New(os.Stderr, "DEBUG: ", 0)
			}

			trace, err := cmd.Flags().GetBool("trace")
			if err != nil {
				errorLogger.Fatalln("cannot parse trace flag:", err)
			}
			if trace {
				traceLogger = log.New(os.Stderr, "TRACE: ", 0)
			}
		},
	}
)

func init() {
	rootCmd.PersistentFlags().DurationP("interval", "i", 1*time.Second, "interval to check for clipboard changes / interact with transport")
	rootCmd.PersistentFlags().StringP("blocksize", "b", "64k", "max data sent per packet via transport")
	rootCmd.PersistentFlags().StringP("password", "p", "cliptun", "password for encrypting the tunnel")
	rootCmd.PersistentFlags().StringP("transport", "t", "clipboard", "transport for tunnel (clipboard|exec=<cmd>|tcp-listen=<addr>:<port>|tcp=<addr>:<port>)")
	rootCmd.PersistentFlags().BoolP("debug", "d", false, "enable debug output")
	rootCmd.PersistentFlags().BoolP("trace", "", false, "trace packets read/written to transport")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		errorLogger.Println(err)
		os.Exit(1)
	}
}
