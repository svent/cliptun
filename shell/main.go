package shell

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/peterh/liner"
	"github.com/pkg/sftp"
	"github.com/svent/cliptun/channel"
	"github.com/svent/cliptun/prompt"
)

var (
	errorLogger = log.New(os.Stderr, "Error: ", 0)
)

func showUsage(cmd prompt.Cmd) {
	fmt.Printf("%s: %s\n", cmd.Name, cmd.Description)
	fmt.Printf("Usage: %s\n", cmd.Usage)
}

func Execute(tunnel *channel.Tunnel) {
	cmds := make(map[string]prompt.Cmd)
	cmds["exec"] = prompt.Cmd{
		Name:        "exec",
		Description: "Execute command on remote system and print output",
		Usage:       "exec <cmd>",
		Run: func(cmd prompt.Cmd, args []string) error {
			if len(args) < 1 {
				showUsage(cmd)
				return nil
			}
			output, err := tunnel.ExecuteCommand(strings.Join(args, " "))
			if err != nil {
				fmt.Println("Error executing command:", err)
				return nil
			}
			fmt.Println(output)

			return nil
		},
	}
	cmds["exit"] = prompt.Cmd{
		Name:        "exit",
		Description: "quit the program",
		Usage:       "exit",
		Run: func(cmd prompt.Cmd, args []string) error {
			return liner.ErrPromptAborted
		},
	}
	cmds["fwd-remote"] = prompt.Cmd{
		Name:        "fwd-remote",
		Description: "fwd remote port",
		Usage:       "fwd-remote <rport> <lhost> <lport>",
		Run: func(cmd prompt.Cmd, args []string) error {
			if len(args) != 3 {
				showUsage(cmd)
				return nil
			}
			var pf channel.PortForwarding
			pf.Port, pf.Host, pf.HostPort = args[0], args[1], args[2]
			tunnel.AddRemotePortForwarding(pf)
			return nil
		},
	}
	cmds["fwd-local"] = prompt.Cmd{
		Name:        "fwd-local",
		Description: "fwd local port",
		Usage:       "fwd-local <lport> <rhost> <rport>",
		Run: func(cmd prompt.Cmd, args []string) error {
			if len(args) != 3 {
				showUsage(cmd)
				return nil
			}
			var pf channel.PortForwarding
			pf.Port, pf.Host, pf.HostPort = args[0], args[1], args[2]
			tunnel.AddLocalPortForwarding(pf)
			return nil
		},
	}
	cmds["help"] = prompt.Cmd{
		Name:        "help",
		Description: "show available commands and command options",
		Usage:       "help",
		Run: func(cmd prompt.Cmd, args []string) error {
			prompt.ShowHelp(cmds, args)
			return nil
		},
	}
	cmds["sftp"] = prompt.Cmd{
		Name:        "sftp",
		Description: "Enter sftp mode",
		Usage:       "sftp",
		Run: func(cmd prompt.Cmd, args []string) error {
			sftpClient := tunnel.StartSftp()
			runSftp(sftpClient)
			return nil
		},
	}
	cmds["socks"] = prompt.Cmd{
		Name:        "socks",
		Description: "Start SOCKS server",
		Usage:       "socks <port>",
		Run: func(cmd prompt.Cmd, args []string) error {
			if len(args) != 1 {
				showUsage(cmd)
				return nil
			}
			port, err := strconv.Atoi(args[0])
			if err != nil {
				errorLogger.Printf("invalid port '%s': %s\n", args[0], err)
				return nil

			}
			tunnel.StartSocksOnPort(port)
			return nil
		},
	}

	p := prompt.NewPrompt(cmds)
	defer p.Close()
	p.Loop("> ")
}

func runSftp(client *sftp.Client) {
	var remoteDir string
	var err error
	if remoteDir, err = client.Getwd(); err != nil {
		errorLogger.Println("cannot get remote current working directory:", err)
		return
	}

	cmds := make(map[string]prompt.Cmd)
	cmds["cd"] = prompt.Cmd{
		Name:        "cd",
		Description: "change remote directory",
		Usage:       "cd",
		Run: func(cmd prompt.Cmd, args []string) error {
			if len(args) != 1 {
				showUsage(cmd)
				return nil
			}
			if path.IsAbs(args[0]) {
				remoteDir = args[0]
			} else {
				remoteDir = path.Join(remoteDir, args[0])
			}
			return nil
		},
	}
	cmds["lcd"] = prompt.Cmd{
		Name:        "lcd",
		Description: "change local directory",
		Usage:       "lcd",
		Run: func(cmd prompt.Cmd, args []string) error {
			if len(args) != 1 {
				showUsage(cmd)
				return nil
			}
			err := os.Chdir(args[0])
			if err != nil {
				errorLogger.Println("cannot set current directory:", err)
				return nil
			}
			return nil
		},
	}
	cmds["ls"] = prompt.Cmd{
		Name:        "ls",
		Description: "list remote directory content",
		Usage:       "ls",
		Run: func(cmd prompt.Cmd, args []string) error {
			entries, err := client.ReadDir(remoteDir)
			if err != nil {
				errorLogger.Println("cannot read directory content:", err)
				return nil
			}
			for _, e := range entries {
				fmt.Printf("% 8s %s %s\n", humanize.Bytes(uint64(e.Size())), e.ModTime().Format("2006-01-02 15:04"), e.Name())
			}
			return nil
		},
	}
	cmds["lls"] = prompt.Cmd{
		Name:        "lls",
		Description: "list local directory content",
		Usage:       "lls",
		Run: func(cmd prompt.Cmd, args []string) error {
			dir, err := os.Getwd()
			if err != nil {
				errorLogger.Println("cannot get current working directory:", err)
				return nil
			}
			files, err := ioutil.ReadDir(dir)
			if err != nil {
				errorLogger.Println("cannot read directory content:", err)
				return nil
			}

			for _, e := range files {
				fmt.Printf("% 8s %s %s\n", humanize.Bytes(uint64(e.Size())), e.ModTime().Format("2006-01-02 15:04"), e.Name())
			}
			return nil
		},
	}
	cmds["pwd"] = prompt.Cmd{
		Name:        "pwd",
		Description: "print remote working directory",
		Usage:       "pwd",
		Run: func(cmd prompt.Cmd, args []string) error {
			fmt.Println(remoteDir)
			return nil
		},
	}
	cmds["lpwd"] = prompt.Cmd{
		Name:        "lpwd",
		Description: "print local working directory",
		Usage:       "lpwd",
		Run: func(cmd prompt.Cmd, args []string) error {
			dir, err := os.Getwd()
			if err != nil {
				fmt.Println("Error getting current working directory:", err)
			}
			fmt.Println(dir)
			return nil
		},
	}
	cmds["download"] = prompt.Cmd{
		Name:        "download",
		Description: "download file",
		Usage:       "download <filename>",
		Run: func(cmd prompt.Cmd, args []string) error {
			if len(args) != 1 {
				showUsage(cmd)
				return nil
			}

			rf, err := client.Open(path.Join(remoteDir, args[0]))
			if err != nil {
				errorLogger.Println("cannot open remote file:", err)
				return nil
			}
			defer rf.Close()

			lf, err := os.Create(args[0])
			if err != nil {
				errorLogger.Println("cannot open local file:", err)
				return nil
			}

			n, err := rf.WriteTo(lf)
			if err != nil {
				errorLogger.Println("cannot complete file transfer:", err)
				return nil
			}
			fmt.Println("transferred", humanize.Bytes(uint64(n)))
			return nil
		},
	}
	cmds["upload"] = prompt.Cmd{
		Name:        "upload",
		Description: "upload file",
		Usage:       "upload <filename>",
		Run: func(cmd prompt.Cmd, args []string) error {
			if len(args) != 1 {
				showUsage(cmd)
				return nil
			}

			lf, err := os.Open(args[0])
			if err != nil {
				errorLogger.Println("cannot open local file:", err)
				return nil
			}
			defer lf.Close()

			rf, err := client.Create(path.Join(remoteDir, args[0]))
			if err != nil {
				errorLogger.Println("cannot open remote file:", err)
				return nil
			}
			defer rf.Close()

			n, err := rf.ReadFrom(lf)
			if err != nil {
				errorLogger.Println("cannot complete file transfer:", err)
				return nil
			}
			fmt.Println("transferred", humanize.Bytes(uint64(n)))
			return nil
		},
	}
	cmds["exit"] = prompt.Cmd{
		Name:        "exit",
		Description: "return to main menu",
		Usage:       "exit",
		Run: func(cmd prompt.Cmd, args []string) error {
			return liner.ErrPromptAborted
		},
	}
	cmds["help"] = prompt.Cmd{
		Name:        "help",
		Description: "show available commands and command options",
		Usage:       "help",
		Run: func(cmd prompt.Cmd, args []string) error {
			prompt.ShowHelp(cmds, args)
			return nil
		},
	}

	p := prompt.NewPrompt(cmds)
	defer p.Close()
	p.Loop("sftp> ")

}
