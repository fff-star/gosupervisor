package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
)

const ProgramName = "gosupervisorctl"

func main() {
	var exitCode int
	defer func() { os.Exit(exitCode) }()

	socketPath := flag.String("socket", "/tmp/gosupervisor.sock", "Unix socket 路径")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		printUsage()
		return
	}

	conn, err := net.Dial("unix", *socketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接 socket 失败 (%s): %v\n", *socketPath, err)
		exitCode = 1
		return
	}
	defer conn.Close()

	cmd := args[0]
	switch cmd {
	case "status":
		if len(args) > 1 {
			sendCommand(conn, fmt.Sprintf("status %s", args[1]))
		} else {
			sendCommand(conn, "status")
		}
	case "start":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "start 命令需要进程名")
			exitCode = 1
			return
		}
		sendCommand(conn, fmt.Sprintf("start %s", args[1]))
	case "stop":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "stop 命令需要进程名")
			exitCode = 1
			return
		}
		sendCommand(conn, fmt.Sprintf("stop %s", args[1]))
	case "restart":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "restart 命令需要进程名")
			exitCode = 1
			return
		}
		sendCommand(conn, fmt.Sprintf("restart %s", args[1]))
	case "group-start":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "group-start 命令需要组名")
			exitCode = 1
			return
		}
		sendCommand(conn, fmt.Sprintf("group-start %s", args[1]))
	case "group-stop":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "group-stop 命令需要组名")
			exitCode = 1
			return
		}
		sendCommand(conn, fmt.Sprintf("group-stop %s", args[1]))
	case "group-restart":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "group-restart 命令需要组名")
			exitCode = 1
			return
		}
		sendCommand(conn, fmt.Sprintf("group-restart %s", args[1]))
	case "help":
		sendCommand(conn, "help")
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n", cmd)
		printUsage()
		exitCode = 1
	}
}

func sendCommand(conn net.Conn, cmd string) {
	fmt.Fprintln(conn, cmd)
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		fmt.Fprintln(os.Stderr, "读取响应失败")
		return
	}
	resp := scanner.Text()
	fmt.Println(resp)
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
  group-start <g>   启动进程组
  group-stop <g>    停止进程组
  group-restart <g> 重启进程组
  help              显示帮助
`, ProgramName, ProgramName)
}
