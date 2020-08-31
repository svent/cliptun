package prompt

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/peterh/liner"
)

type Prompt struct {
	l    *liner.State
	cmds map[string]Cmd
}

type Cmd struct {
	Name        string
	Description string
	Usage       string
	Run         func(cmd Cmd, args []string) error
}

func NewPrompt(cmds map[string]Cmd) *Prompt {
	p := Prompt{l: liner.NewLiner()}
	p.l.SetCompleter(p.completer)
	p.l.SetCtrlCAborts(true)

	p.cmds = cmds
	return &p
}

func (p *Prompt) Close() {
	p.l.Close()
}

func (p *Prompt) Prompt(prompt string) (string, error) {
	line, err := p.l.Prompt(prompt)
	if err == nil {
		p.l.AppendHistory(line)
	}
	return line, err
}

func (p *Prompt) Loop(prompt string) {
	for {
		if line, err := p.Prompt(prompt); err == nil {
			line := strings.TrimSpace(line)
			if line == "" {
				continue
			}
			w := strings.Split(line, " ")
			c := w[0]
			cmd, ok := p.cmds[c]
			if !ok {
				fmt.Println("unknown command")
				continue
			}
			err := cmd.Run(cmd, w[1:])
			if err == liner.ErrPromptAborted {
				return
			}
		} else if err == liner.ErrPromptAborted {
			return
		} else if err == io.EOF {
			fmt.Println("")
			return
		} else {
			fmt.Println("")
		}
	}
}

func (p *Prompt) completer(line string) (c []string) {
	if strings.HasPrefix(strings.ToLower(line), "help") {
		for k, _ := range p.cmds {
			if line == "help" || strings.HasPrefix("help "+k, strings.ToLower(line)) {
				c = append(c, "help "+k)
			}
		}
		return c
	}
	for k, _ := range p.cmds {
		if strings.HasPrefix(k, strings.ToLower(line)) {
			c = append(c, k)
		}
	}
	return c
}

func ShowHelp(cmds map[string]Cmd, args []string) {
	if len(args) == 0 {
		fmt.Println("Available Commands:")

		cmdlist := make([]string, len(cmds))
		i := 0
		for c := range cmds {
			cmdlist[i] = c
			i++
		}
		sort.Strings(cmdlist)

		for _, k := range cmdlist {
			fmt.Printf("  %s: %s\n", k, cmds[k].Description)
		}
		fmt.Println(`Use "help <cmd>" for more information about a command.`)
		return
	}
	c := args[0]
	v, ok := cmds[c]
	if !ok {
		fmt.Println("unknown command")
		return
	}
	fmt.Printf("%s: %s\n", c, v.Description)
	fmt.Printf("Usage: %s\n", v.Usage)
	return
}
