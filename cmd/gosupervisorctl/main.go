package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/peterh/liner"
)

const ProgramName = "gosupervisorctl"

var commands = []string{
	"status", "start", "stop", "restart", "signal",
	"reload", "events", "group-start", "group-stop", "group-restart",
	"help", "quit", "exit",
}

func main() {
	var exitCode int
	defer func() { os.Exit(exitCode) }()

	socketPath := flag.String("socket", "/tmp/gosupervisor.sock", "Unix socket 路径")
	flag.Parse()

	args := flag.Args()
	if len(args) > 0 {
		// One-shot mode (backward compatible)
		conn, err := net.Dial("unix", *socketPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "连接 socket 失败 (%s): %v\n", *socketPath, err)
			exitCode = 1
			return
		}
		defer conn.Close()
		handleCommand(conn, args)
		return
	}

	// REPL mode
	runREPL(*socketPath)
}

func handleCommand(conn net.Conn, args []string) {
	cmd := args[0]
	switch cmd {
	case "status":
		if len(args) > 1 {
			sendLine(conn, fmt.Sprintf("status %s", args[1]))
		} else {
			sendLine(conn, "status")
		}
	case "start":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "start 命令需要进程名")
			return
		}
		sendLine(conn, fmt.Sprintf("start %s", args[1]))
	case "stop":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "stop 命令需要进程名")
			return
		}
		sendLine(conn, fmt.Sprintf("stop %s", args[1]))
	case "restart":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "restart 命令需要进程名")
			return
		}
		sendLine(conn, fmt.Sprintf("restart %s", args[1]))
	case "signal":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "signal 命令需要进程名和信号名")
			return
		}
		sendLine(conn, fmt.Sprintf("signal %s %s", args[1], args[2]))
	case "reload":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "reload 命令需要进程名")
			return
		}
		sendLine(conn, fmt.Sprintf("reload %s", args[1]))
	case "events":
		if len(args) > 1 {
			sendLine(conn, fmt.Sprintf("events %s", args[1]))
		} else {
			sendLine(conn, "events")
		}
	case "group-start":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "group-start 命令需要组名")
			return
		}
		sendLine(conn, fmt.Sprintf("group-start %s", args[1]))
	case "group-stop":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "group-stop 命令需要组名")
			return
		}
		sendLine(conn, fmt.Sprintf("group-stop %s", args[1]))
	case "group-restart":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "group-restart 命令需要组名")
			return
		}
		sendLine(conn, fmt.Sprintf("group-restart %s", args[1]))
	case "help":
		sendLine(conn, "help")
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n", cmd)
		printUsage()
	}
}

func sendLine(conn net.Conn, cmd string) {
	fmt.Fprintln(conn, cmd)
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		fmt.Fprintln(os.Stderr, "读取响应失败")
		return
	}
	fmt.Println(scanner.Text())
}

// runREPL runs the interactive REPL loop.
func runREPL(socketPath string) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接 socket 失败 (%s): %v\n", socketPath, err)
		return
	}
	defer conn.Close()

	fmt.Printf("GoSupervisor REPL — 连接到 %s，输入 'quit' 退出\n", socketPath)

	line := liner.NewLiner()
	defer line.Close()

	line.SetCtrlCAborts(true)
	line.SetCompleter(buildCompleter(conn))

	// Load history
	historyFile := historyPath()
	if f, err := os.Open(historyFile); err == nil {
		line.ReadHistory(f)
		f.Close()
	}

	for {
		input, err := line.Prompt("gosupervisor> ")
		if err != nil {
			if err == liner.ErrPromptAborted {
				fmt.Println("^C")
				continue
			}
			break
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		line.AppendHistory(input)

		if input == "quit" || input == "exit" {
			break
		}

		if input == "help" {
			printREPLHelp()
			continue
		}

		// Send command to socket
		fmt.Fprintln(conn, input)
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			resp := scanner.Text()
			fmt.Println(resp)
			// socket server sends single-line responses, so one line is enough
			break
		}
	}

	// Save history
	if f, err := os.Create(historyFile); err == nil {
		line.WriteHistory(f)
		f.Close()
	}
	fmt.Println("再见!")
}

// buildCompleter returns a dynamic tab-completion function.
func buildCompleter(conn net.Conn) func(line string) []string {
	// Fetch process names from the server once on startup
	processNames := fetchProcessNames(conn)

	return func(line string) []string {
		var completions []string
		fields := strings.Fields(line)
		if len(fields) == 0 {
			return completions
		}

		prefix := strings.ToLower(fields[len(fields)-1])

		// First word: complete commands
		if len(fields) == 1 {
			for _, cmd := range commands {
				if strings.HasPrefix(cmd, prefix) {
					completions = append(completions, cmd)
				}
			}
		}

		// Second word: complete process names for process-targeting commands
		if len(fields) == 2 {
			switch fields[0] {
			case "start", "stop", "restart", "reload", "status":
				for _, name := range processNames {
					if strings.HasPrefix(name, prefix) {
						completions = append(completions, name)
					}
				}
			case "signal":
				for _, name := range processNames {
					if strings.HasPrefix(name, prefix) {
						completions = append(completions, name)
					}
				}
			}
		}

		// Third word for signal command: common signal names
		if len(fields) == 3 && fields[0] == "signal" {
			for _, sig := range []string{"SIGTERM", "SIGKILL", "SIGHUP", "SIGINT", "SIGQUIT", "SIGUSR1", "SIGUSR2"} {
				if strings.HasPrefix(strings.ToUpper(sig), strings.ToUpper(prefix)) {
					completions = append(completions, sig)
				}
			}
		}

		return completions
	}
}

// fetchProcessNames queries the server for the list of process names.
func fetchProcessNames(conn net.Conn) []string {
	fmt.Fprintln(conn, "status")
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		return nil
	}
	// Response: "OK 5 processes" then one line per process
	_ = scanner.Text() // consume the "OK N processes" line
	var names []string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		// Line format: "name STATE pid=N restarts=N"
		parts := strings.Fields(line)
		if len(parts) > 0 {
			names = append(names, parts[0])
		}
	}
	return names
}

func historyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gosupervisorctl_history")
}

func printREPLHelp() {
	fmt.Println("命令:")
	fmt.Println("  status [name]      显示进程状态")
	fmt.Println("  start <name>       启动进程")
	fmt.Println("  stop <name>        停止进程")
	fmt.Println("  restart <name>     重启进程")
	fmt.Println("  signal <name> <sig> 发送信号")
	fmt.Println("  reload <name>      重载进程 (SIGHUP)")
	fmt.Println("  events [N]         显示事件")
	fmt.Println("  group-start <g>    启动进程组")
	fmt.Println("  group-stop <g>     停止进程组")
	fmt.Println("  group-restart <g>  重启进程组")
	fmt.Println("  help               显示帮助")
	fmt.Println("  quit/exit          退出")
}

func printUsage() {
	fmt.Printf(`%s — GoSupervisor socket CLI 客户端

用法: %s [options] <command> [args]

选项:
  -socket string     Unix socket 路径 (默认 "/tmp/gosupervisor.sock")

命令:
  status [name]     显示所有进程或指定进程状态
  start <name>      启动进程
  stop <name>       停止进程
  restart <name>    重启进程
  signal <name> <sig> 向进程发送信号
  reload <name>     向进程发送 SIGHUP 重载信号
  events [N]        显示最近 N 条事件 (默认 50)
  group-start <g>   启动进程组
  group-stop <g>    停止进程组
  group-restart <g> 重启进程组
  help              显示帮助

不带命令参数则进入交互式 REPL 模式。
`, ProgramName, ProgramName)
}
