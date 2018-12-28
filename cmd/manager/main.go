/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"runtime"
	"time"

	goflag "flag"
	opflags "github.com/grepplabs/aws-resource-operator/pkg/flags"
	flag "github.com/spf13/pflag"

	"github.com/grepplabs/aws-resource-operator/pkg/apis"
	"github.com/grepplabs/aws-resource-operator/pkg/controller"
	"github.com/grepplabs/aws-resource-operator/pkg/version"
	"github.com/grepplabs/aws-resource-operator/pkg/webhook"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
)

var (
	leaderElection          = flag.Bool("leader-election", false, "Determines whether or not the controller use leader election")
	leaderElectionID        = flag.String("leader-election-id", "", "Name of the configmap that leader election will use for holding the leader lock")
	leaderElectionNamespace = flag.String("leader-election-namespace", "", "Namespace in which the leader election configmap will be created")
	metricsAddr             = flag.String("metrics-addr", ":8080", "TCP address that the controller should bind to for serving prometheus metrics")
	syncPeriod              = flag.Duration("sync-period", 5*time.Minute, "Reconcile sync period")
	jsonLogger              = flag.Bool("json-logger", false, "Use json logger (production)")
)

func main() {
	// Setup flags
	_ = goflag.Lookup("logtostderr").Value.Set("true")
	flag.CommandLine.AddFlagSet(opflags.FlagSet)
	flag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	flag.Parse()

	logf.SetLogger(GetLogger(!*jsonLogger))
	log := logf.Log.WithName("entrypoint")

	// Print version
	log.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	log.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))
	log.Info(fmt.Sprintf("AWS Resource Operator Version: %v", version.Version))

	// Get a config to talk to the apiserver
	log.Info("Setting up client for manager")
	cfg, err := config.GetConfig()
	if err != nil {
		log.Error(err, "unable to set up client config")
		os.Exit(1)
	}

	// Create a new Cmd to provide shared dependencies and start components
	log.Info("setting up manager")
	mgr, err := manager.New(cfg, manager.Options{
		LeaderElection:          *leaderElection,
		LeaderElectionID:        *leaderElectionID,
		LeaderElectionNamespace: *leaderElectionNamespace,
		MetricsBindAddress:      *metricsAddr,
		SyncPeriod:              syncPeriod,
		Namespace:               opflags.Namespace,
	})
	if err != nil {
		log.Error(err, "unable to set up overall controller manager")
		os.Exit(1)
	}

	log.Info("Registering Components.")

	// Setup Scheme for all resources
	log.Info("Setting up scheme")
	if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
		log.Error(err, "unable add APIs to scheme")
		os.Exit(1)
	}

	// Setup all Controllers
	log.Info("Setting up controller")
	if err := controller.AddToManager(mgr); err != nil {
		log.Error(err, "unable to register controllers to the manager")
		os.Exit(1)
	}

	log.Info("Setting up webhooks")
	if err := webhook.AddToManager(mgr); err != nil {
		log.Error(err, "unable to register webhooks to the manager")
		os.Exit(1)
	}

	// Start the Cmd
	log.Info("Starting the Cmd.")
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		log.Error(err, "unable to run the manager")
		os.Exit(1)
	}
}

func GetLogger(development bool) logr.Logger {
	sink := zapcore.AddSync(os.Stderr)

	var enc zapcore.Encoder
	var lvl zap.AtomicLevel
	var opts []zap.Option
	if development {
		encCfg := zap.NewDevelopmentEncoderConfig()
		enc = zapcore.NewConsoleEncoder(encCfg)
		lvl = zap.NewAtomicLevelAt(zap.DebugLevel)
		opts = append(opts, zap.Development(), zap.AddStacktrace(zap.ErrorLevel))
	} else {
		encCfg := zap.NewProductionEncoderConfig()
		encCfg.TimeKey = "@timestamp"
		encCfg.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.Format("2006-01-02T15:04:05.000Z"))
		}
		enc = zapcore.NewJSONEncoder(encCfg)
		lvl = zap.NewAtomicLevelAt(zap.InfoLevel)
		opts = append(opts, zap.AddStacktrace(zap.WarnLevel),
			zap.WrapCore(func(core zapcore.Core) zapcore.Core {
				return zapcore.NewSampler(core, time.Second, 100, 100)
			}))
	}
	opts = append(opts, zap.AddCallerSkip(1), zap.ErrorOutput(sink))
	log := zap.New(zapcore.NewCore(&logf.KubeAwareEncoder{Encoder: enc, Verbose: development}, sink, lvl))
	log = log.WithOptions(opts...)
	return zapr.NewLogger(log)
}
