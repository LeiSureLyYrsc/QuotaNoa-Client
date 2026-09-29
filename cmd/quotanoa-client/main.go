// Command quotanoa-client is the QuotaNoa remote client: it dials the Bot's
// Server_Mode WebSocket, reports its version/capabilities, and serves quota
// queries plus locally-gated Codex reset requests.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/agent"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/config"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/protocol"
	"github.com/LeiSureLyYrsc/QuotaNoa-Client/internal/version"
)

var (
	configPath     string
	logLevel       string
	generateConfig string
	forceWrite     bool
)

func main() {
	root := &cobra.Command{
		Use:          "quotanoa-client",
		Short:        "QuotaNoa remote quota client",
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if generateConfig != "" {
				runGenerate(generateConfig, forceWrite)
			}
			return nil
		},
	}
	root.PersistentFlags().StringVar(&configPath, "config", envOr("QUOTANOA_CONFIG", "config.json"), "配置文件路径")
	root.PersistentFlags().StringVar(&logLevel, "log-level", "info", "日志级别 (debug/info/warn/error)")
	root.PersistentFlags().StringVar(&generateConfig, "generate-config", "", "生成默认配置文件并退出（可带路径，默认 config.json）")
	root.PersistentFlags().BoolVar(&forceWrite, "force", false, "覆盖已存在的文件")
	if flag := root.PersistentFlags().Lookup("generate-config"); flag != nil {
		flag.NoOptDefVal = "config.json"
	}

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "连接服务端并常驻",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgent()
		},
	}
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "打印版本",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("QuotaNoa Client %s (protocol v%d)\n", version.Version, protocol.Version)
		},
	}
	configCmd := &cobra.Command{Use: "config", Short: "配置工具"}
	var initOut string
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "生成默认配置文件",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.SaveDefault(initOut, forceWrite); err != nil {
				return err
			}
			abs, _ := filepath.Abs(initOut)
			fmt.Printf("已生成默认配置文件：%s\n请填写 client.server_url 与 client.key 后运行：quotanoa-client run --config %s\n", abs, initOut)
			return nil
		},
	}
	initCmd.Flags().StringVar(&initOut, "out", "config.json", "输出路径")
	configCmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "校验配置并打印摘要（不联网）",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			fmt.Print(cfg.Redacted())
			return nil
		},
	}, initCmd)

	root.AddCommand(runCmd, versionCmd, configCmd)
	root.RunE = runCmd.RunE
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func runAgent() error {
	setupLogger(logLevel)
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if cfg.Client.Key == "" {
		return fmt.Errorf("未配置 client.key（或环境变量 QUOTANOA_CLIENT_KEY）")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return agent.New(cfg).Run(ctx)
}

func setupLogger(level string) {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})))
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// runGenerate writes a default config file and exits (used by --generate-config).
func runGenerate(path string, force bool) {
	if err := config.SaveDefault(path, force); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	abs, _ := filepath.Abs(path)
	fmt.Printf("已生成默认配置文件：%s\n请填写 client.server_url 与 client.key 后运行：quotanoa-client run --config %s\n", abs, path)
	os.Exit(0)
}
