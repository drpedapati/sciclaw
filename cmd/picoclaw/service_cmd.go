package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	svcmgr "github.com/sipeed/picoclaw/pkg/service"
)

type serviceLogsOptions struct {
	Lines int
}

func serviceCmd() {
	args := os.Args[2:]
	if len(args) == 0 {
		serviceHelp()
		return
	}

	sub := strings.ToLower(args[0])
	if sub == "help" || sub == "--help" || sub == "-h" {
		serviceHelp()
		return
	}

	exePath, err := os.Executable()
	if err != nil {
		fmt.Printf("Error resolving executable path: %v\n", err)
		os.Exit(1)
	}

	mgr, err := svcmgr.NewManager(exePath)
	if err != nil {
		fmt.Printf("Error initializing service manager: %v\n", err)
		os.Exit(1)
	}

	switch sub {
	case "install":
		if err := mgr.Install(); err != nil {
			fmt.Printf("Service install failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓ Service installed")
		fmt.Printf("  Start with: %s service start\n", invokedCLIName())
	case "uninstall", "remove":
		if err := mgr.Uninstall(); err != nil {
			fmt.Printf("Service uninstall failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓ Service uninstalled")
	case "start":
		if err := mgr.Start(); err != nil {
			fmt.Printf("Service start failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓ Service started")
	case "stop":
		if err := mgr.Stop(); err != nil {
			fmt.Printf("Service stop failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓ Service stopped")
	case "restart":
		if err := mgr.Restart(); err != nil {
			fmt.Printf("Service restart failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓ Service restarted")
	case "status":
		st, err := mgr.Status()
		if err != nil {
			fmt.Printf("Service status check failed: %v\n", err)
			os.Exit(1)
		}
		printServiceStatus(st)
	case "logs":
		opts, showHelp, err := parseServiceLogsOptions(args[1:])
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			serviceHelp()
			os.Exit(2)
		}
		if showHelp {
			serviceHelp()
			return
		}
		out, err := mgr.Logs(opts.Lines)
		if err != nil {
			fmt.Printf("Service logs failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(out)
	default:
		fmt.Printf("Unknown service command: %s\n", sub)
		serviceHelp()
		os.Exit(2)
	}
}

func serviceHelp() {
	commandName := invokedCLIName()
	fmt.Println("\nService commands:")
	fmt.Println("  install             Install background gateway service")
	fmt.Println("  uninstall           Remove background gateway service")
	fmt.Println("  start               Start background gateway service")
	fmt.Println("  stop                Stop background gateway service")
	fmt.Println("  restart             Restart background gateway service")
	fmt.Println("  status              Show service install/runtime status")
	fmt.Println("  logs                Show recent service logs")
	fmt.Println()
	fmt.Println("Logs options:")
	fmt.Println("  -n, --lines <N>     Number of log lines to show (default: 100)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Printf("  %s service install\n", commandName)
	fmt.Printf("  %s service start\n", commandName)
	fmt.Printf("  %s service status\n", commandName)
	fmt.Printf("  %s service logs --lines 200\n", commandName)
	fmt.Printf("  (Compatibility alias also works: %s)\n", cliName)
}

func parseServiceLogsOptions(args []string) (serviceLogsOptions, bool, error) {
	opts := serviceLogsOptions{Lines: 100}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n", "--lines":
			if i+1 >= len(args) {
				return opts, false, fmt.Errorf("%s requires a value", args[i])
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				return opts, false, fmt.Errorf("invalid value for %s: %q", args[i], args[i+1])
			}
			opts.Lines = n
			i++
		case "help", "--help", "-h":
			return opts, true, nil
		default:
			return opts, false, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	return opts, false, nil
}

func printServiceStatus(st svcmgr.Status) {
	yn := func(v bool) string {
		if v {
			return "yes"
		}
		return "no"
	}

	fmt.Println("\nGateway service status:")
	fmt.Printf("  Backend:   %s\n", st.Backend)
	fmt.Printf("  Installed: %s\n", yn(st.Installed))
	fmt.Printf("  Running:   %s\n", yn(st.Running))
	fmt.Printf("  Enabled:   %s\n", yn(st.Enabled))
	if strings.TrimSpace(st.Detail) != "" {
		fmt.Printf("  Detail:    %s\n", st.Detail)
	}
}
