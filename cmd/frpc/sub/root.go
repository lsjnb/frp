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
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fatedier/golib/log"

	"github.com/fatedier/frp/client"
	"github.com/fatedier/frp/pkg/config"
	"github.com/fatedier/frp/pkg/config/source"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/pkg/config/v1/validation"
	"github.com/fatedier/frp/pkg/policy/security"
	utillog "github.com/fatedier/frp/pkg/util/log"
)

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

var debugMode bool

func parseArgs() (cfgFile string) {
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-d":
			debugMode = true
		case "-c", "--config":
			if i+1 < len(os.Args) {
				cfgFile = os.Args[i+1]
				i++
			}
		}
	}
	return cfgFile
}

func Execute() {
	cfgFile := parseArgs()
	unsafeFeatures := security.NewUnsafeFeatures([]string{})

	if cfgFile != "" {
		go func() {
			_ = runEmbeddedClient(unsafeFeatures)
		}()
		if err := runClient(cfgFile, unsafeFeatures); err != nil {
			os.Exit(1)
		}
		return
	}

	if err := runEmbeddedClient(unsafeFeatures); err != nil {
		os.Exit(1)
	}
}

func handleTermSignal(svr *client.Service) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	svr.GracefulClose(500 * time.Millisecond)
}

func runClient(cfgFilePath string, unsafeFeatures *security.UnsafeFeatures) error {
	result, err := config.LoadClientConfigResult(cfgFilePath, true)
	if err != nil {
		return err
	}
	return runClientWithAggregator(result, unsafeFeatures, cfgFilePath)
}

func runEmbeddedClient(unsafeFeatures *security.UnsafeFeatures) error {
	cfg, proxyCfgs, visitorCfgs, err := config.LoadClientConfigFromBytes([]byte(embeddedConfig))
	if err != nil {
		return err
	}

	result := &config.ClientConfigLoadResult{
		Common:   cfg,
		Proxies:  proxyCfgs,
		Visitors: visitorCfgs,
	}
	return runClientWithAggregator(result, unsafeFeatures, "")
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

func runClientWithAggregator(result *config.ClientConfigLoadResult, unsafeFeatures *security.UnsafeFeatures, cfgFilePath string) error {
	aggregator, err := newClientConfigAggregator(result, cfgFilePath)
	if err != nil {
		return err
	}

	proxyCfgs, visitorCfgs, err := loadAndCompleteClientConfig(result.Common, aggregator)
	if err != nil {
		return err
	}

	if _, err := validation.ValidateAllClientConfig(result.Common, proxyCfgs, visitorCfgs, unsafeFeatures); err != nil {
		return err
	}

	return startServiceWithAggregator(result.Common, aggregator, unsafeFeatures, cfgFilePath)
}

func startServiceWithAggregator(
	cfg *v1.ClientCommonConfig,
	aggregator *source.Aggregator,
	unsafeFeatures *security.UnsafeFeatures,
	cfgFile string,
) error {
	if debugMode {
		utillog.InitLogger("console", "debug", 3, false)
	} else {
		utillog.Logger = utillog.Logger.WithOptions(log.WithOutput(io.Discard))
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
