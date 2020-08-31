package cmd

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/svent/cliptun/channel"
)

func getChannelOptions(cmd *cobra.Command) (channel.ChannelOptions, error) {
	// basic flag parsing errors will be handled by cobra
	interval, _ := cmd.Flags().GetDuration("interval")
	password, _ := cmd.Flags().GetString("password")
	transport, _ := cmd.Flags().GetString("transport")
	bs, _ := cmd.Flags().GetString("blocksize")
	blocksize, err := parseBlocksize(bs)
	if err != nil {
		return channel.ChannelOptions{}, fmt.Errorf("cannot parse blocksize: %s", err)
	}
	options := channel.ChannelOptions{
		Interval:    interval,
		Password:    password,
		Transport:   transport,
		Blocksize:   blocksize,
		ErrorLogger: errorLogger,
		DebugLogger: debugLogger,
		TraceLogger: traceLogger,
	}
	return options, nil
}

func parseBlocksize(arg string) (int, error) {
	re := regexp.MustCompile(`^\d+[kKmM]?$`)
	if !re.MatchString(arg) {
		return 0, fmt.Errorf("unknown blocksize format '%s'", arg)
	}
	var blocksize int
	switch arg[len(arg)-1:] {
	case "k", "K":
		blocksize, _ = strconv.Atoi(arg[0 : len(arg)-1])
		return blocksize * 1024, nil
	case "m", "M":
		blocksize, _ = strconv.Atoi(arg[0 : len(arg)-1])
		return blocksize * 1024 * 1024, nil
	default:
		blocksize, _ := strconv.Atoi(arg)
		return blocksize, nil
	}
}
