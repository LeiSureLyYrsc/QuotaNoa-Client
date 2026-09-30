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
	patchCmd := &cobra.Command{
		Use:   "patch",
		Short: "备份并补齐配置文件缺失项，写入当前配置版本号",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := config.Patch(configPath)
			if err != nil {
				return err
			}
			if !result.Changed {
				fmt.Printf(
					"配置已完整（config_version=%d），无需修补。\n配置文件：%s\n",
					result.Version, result.Path,
				)
				return nil
			}
			abs, _ := filepath.Abs(result.Path)
			backupAbs, _ := filepath.Abs(result.BackupPath)
			fmt.Printf("配置已修补：%s\n", abs)
			fmt.Printf("备份：%s\n", backupAbs)
			fmt.Printf("config_version：%d\n", result.Version)
			if len(result.AddedKeys) > 0 {
				fmt.Println("补齐的键：")
				for _, key := range result.AddedKeys {
					fmt.Printf("  + %s\n", key)
				}
			}
			return nil
		},
	}
	var (
		addName         string
		addBaseURL      string
		addKey          string
		addTimeout      float64
		addQuotaTimeout float64
		addConcurrency  int
		addCacheTTL     float64
		addNoImage      bool
		addAK           string
		addSK           string
		addRegion       string
		addUsername     string
		addPassword     string
		addAPIKey       string
		removeName      string
	)
	addCmd := &cobra.Command{
		Use:   "add <cpa|volc|wb|qoder> [flags]",
		Short: "添加实例/账号（CPA / 火山 / WorkBuddy / Qoder）",
		Long: "添加实例/账号到配置文件。类型别名：cpa、volc/火山、wb/workbuddy、qoder。\n" +
			"写入前会自动备份原配置。",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, ok := config.NormalizeSection(args[0])
			if !ok {
				return fmt.Errorf("未知类型：%s（可选 cpa / volc / wb / qoder）", args[0])
			}
			entry, err := buildEntry(key, entryInput{
				name: addName, baseURL: addBaseURL, key: addKey,
				timeout: addTimeout, quotaTimeout: addQuotaTimeout,
				concurrency: addConcurrency, cacheTTL: addCacheTTL, noImage: addNoImage,
				ak: addAK, sk: addSK, region: addRegion,
				username: addUsername, password: addPassword, apiKey: addAPIKey,
			})
			if err != nil {
				return err
			}
			backup, err := config.AddEntry(configPath, key, addName, entry)
			if err != nil {
				return err
			}
			abs, _ := filepath.Abs(configPath)
			fmt.Printf("已添加 %s：%s\n配置文件：%s\n", config.SectionLabel(key), strings.TrimSpace(addName), abs)
			if backup != "" {
				backupAbs, _ := filepath.Abs(backup)
				fmt.Printf("备份：%s\n", backupAbs)
			}
			return nil
		},
	}
	addCmd.Flags().StringVar(&addName, "name", "", "名称（必填，需唯一）")
	addCmd.Flags().StringVar(&addBaseURL, "base-url", "", "服务地址（cpa/wb/qoder 必填）")
	addCmd.Flags().StringVar(&addKey, "key", "", "CPA management_key")
	addCmd.Flags().Float64Var(&addTimeout, "timeout", 0, "请求超时秒")
	addCmd.Flags().Float64Var(&addQuotaTimeout, "quota-timeout", 0, "额度查询超时秒（cpa）")
	addCmd.Flags().IntVar(&addConcurrency, "concurrency", 0, "额度查询并发（cpa）")
	addCmd.Flags().Float64Var(&addCacheTTL, "cache-ttl", 0, "实例级缓存秒，0=跟随 refreshcache（cpa）")
	addCmd.Flags().BoolVar(&addNoImage, "no-image", false, "该实例关闭图片渲染（cpa）")
	addCmd.Flags().StringVar(&addAK, "ak", "", "火山 AccessKey ID")
	addCmd.Flags().StringVar(&addSK, "sk", "", "火山 SecretAccessKey")
	addCmd.Flags().StringVar(&addRegion, "region", "cn-beijing", "火山 region")
	addCmd.Flags().StringVar(&addUsername, "username", "", "WorkBuddy 用户名")
	addCmd.Flags().StringVar(&addPassword, "password", "", "WorkBuddy 密码")
	addCmd.Flags().StringVar(&addAPIKey, "api-key", "", "WorkBuddy / Qoder api_key")

	listCmd := &cobra.Command{
		Use:   "list [cpa|volc|wb|qoder]",
		Short: "列出配置中的实例/账号（省略类型则列出全部）",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keys := config.SectionOrder
			if len(args) == 1 {
				key, ok := config.NormalizeSection(args[0])
				if !ok {
					return fmt.Errorf("未知类型：%s（可选 cpa / volc / wb / qoder）", args[0])
				}
				keys = []string{key}
			}
			abs, _ := filepath.Abs(configPath)
			fmt.Printf("配置文件：%s\n", abs)
			for i, key := range keys {
				if i > 0 {
					fmt.Println()
				}
				entries, err := config.ListEntries(configPath, key)
				if err != nil {
					return err
				}
				printEntries(key, entries)
			}
			return nil
		},
	}

	removeCmd := &cobra.Command{
		Use:     "remove <cpa|volc|wb|qoder> --name <名称>",
		Aliases: []string{"rm"},
		Short:   "按名称删除实例/账号（写入前自动备份）",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, ok := config.NormalizeSection(args[0])
			if !ok {
				return fmt.Errorf("未知类型：%s（可选 cpa / volc / wb / qoder）", args[0])
			}
			if strings.TrimSpace(removeName) == "" {
				return fmt.Errorf("必须用 --name 指定要删除的名称")
			}
			removed, backup, err := config.RemoveEntry(configPath, key, removeName)
			if err != nil {
				return err
			}
			if !removed {
				fmt.Printf("未找到 %s：%s（未改动）\n", config.SectionLabel(key), strings.TrimSpace(removeName))
				return nil
			}
			fmt.Printf("已删除 %s：%s\n", config.SectionLabel(key), strings.TrimSpace(removeName))
			if backup != "" {
				backupAbs, _ := filepath.Abs(backup)
				fmt.Printf("备份：%s\n", backupAbs)
			}
			return nil
		},
	}
	removeCmd.Flags().StringVar(&removeName, "name", "", "要删除的名称（必填）")

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
	}, initCmd, patchCmd, addCmd, listCmd, removeCmd)

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

// entryInput collects the flags for `config add`.
type entryInput struct {
	name         string
	baseURL      string
	key          string
	timeout      float64
	quotaTimeout float64
	concurrency  int
	cacheTTL     float64
	noImage      bool
	ak           string
	sk           string
	region       string
	username     string
	password     string
	apiKey       string
}

// buildEntry validates flags and builds the JSON entry for a section.
func buildEntry(section string, in entryInput) (map[string]any, error) {
	name := strings.TrimSpace(in.name)
	if name == "" {
		return nil, fmt.Errorf("--name 必填")
	}
	entry := map[string]any{"name": name}
	switch section {
	case "cpa":
		base := strings.TrimSpace(in.baseURL)
		if base == "" {
			return nil, fmt.Errorf("--base-url 必填（CPA 实例）")
		}
		entry["base_url"] = base
		if in.key != "" {
			entry["management_key"] = in.key
		}
		if in.timeout > 0 {
			entry["timeout"] = in.timeout
		}
		if in.quotaTimeout > 0 {
			entry["quota_timeout"] = in.quotaTimeout
		}
		if in.concurrency > 0 {
			entry["quota_concurrency"] = in.concurrency
		}
		if in.cacheTTL > 0 {
			entry["quota_cache_ttl"] = in.cacheTTL
		}
		if in.noImage {
			entry["quota_image"] = false
		}
	case "volcengine":
		if strings.TrimSpace(in.ak) == "" || strings.TrimSpace(in.sk) == "" {
			return nil, fmt.Errorf("--ak 与 --sk 必填（火山账号）")
		}
		entry["access_key_id"] = strings.TrimSpace(in.ak)
		entry["secret_access_key"] = strings.TrimSpace(in.sk)
		region := strings.TrimSpace(in.region)
		if region == "" {
			region = "cn-beijing"
		}
		entry["region"] = region
	case "workbuddy":
		base := strings.TrimSpace(in.baseURL)
		if base == "" {
			return nil, fmt.Errorf("--base-url 必填（WorkBuddy 网关）")
		}
		entry["base_url"] = base
		if in.username != "" {
			entry["username"] = in.username
		}
		if in.password != "" {
			entry["password"] = in.password
		}
		if in.apiKey != "" {
			entry["api_key"] = in.apiKey
		}
		if in.timeout > 0 {
			entry["timeout"] = in.timeout
		}
	case "qoder":
		base := strings.TrimSpace(in.baseURL)
		if base == "" {
			return nil, fmt.Errorf("--base-url 必填（Qoder 代理）")
		}
		entry["base_url"] = base
		if in.apiKey != "" {
			entry["api_key"] = in.apiKey
		}
		if in.timeout > 0 {
			entry["timeout"] = in.timeout
		}
	default:
		return nil, fmt.Errorf("未知类型：%s", section)
	}
	return entry, nil
}

// printEntries renders one section's entries for `config list`.
func printEntries(section string, entries []map[string]any) {
	label := config.SectionLabel(section)
	if len(entries) == 0 {
		fmt.Printf("%s：0 个\n", label)
		return
	}
	fmt.Printf("%s：%d 个\n", label, len(entries))
	for i, entry := range entries {
		name, _ := entry["name"].(string)
		detail := entryDetail(section, entry)
		if detail != "" {
			fmt.Printf("  %d) %s  %s\n", i+1, name, detail)
		} else {
			fmt.Printf("  %d) %s\n", i+1, name)
		}
	}
}

func entryDetail(section string, entry map[string]any) string {
	str := func(key string) string {
		value, _ := entry[key].(string)
		return value
	}
	parts := []string{}
	switch section {
	case "cpa":
		if v := str("base_url"); v != "" {
			parts = append(parts, v)
		}
		if v := str("management_key"); v != "" {
			parts = append(parts, "key="+config.Mask(v))
		}
	case "volcengine":
		if v := str("access_key_id"); v != "" {
			parts = append(parts, "ak="+config.Mask(v))
		}
		if v := str("region"); v != "" {
			parts = append(parts, "region="+v)
		}
	case "workbuddy":
		if v := str("base_url"); v != "" {
			parts = append(parts, v)
		}
		if v := str("username"); v != "" {
			parts = append(parts, "user="+v)
		}
		if v := str("api_key"); v != "" {
			parts = append(parts, "api_key="+config.Mask(v))
		}
	case "qoder":
		if v := str("base_url"); v != "" {
			parts = append(parts, v)
		}
		if v := str("api_key"); v != "" {
			parts = append(parts, "api_key="+config.Mask(v))
		}
	}
	return strings.Join(parts, "  ")
}
