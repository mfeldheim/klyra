// cmd/root.go
package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"

	_ "github.com/mfeldheim/klyra/internal/action/http"
	_ "github.com/mfeldheim/klyra/internal/action/pushover"
	_ "github.com/mfeldheim/klyra/internal/monitor/cloudwatch"
	_ "github.com/mfeldheim/klyra/internal/monitor/http"
	_ "github.com/mfeldheim/klyra/internal/monitor/kubernetes"
	_ "github.com/mfeldheim/klyra/internal/monitor/prometheus"
	_ "github.com/mfeldheim/klyra/internal/monitor/promscr"

	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/diagnostics"
	"github.com/mfeldheim/klyra/internal/engine"
	"github.com/mfeldheim/klyra/internal/incident"
	"github.com/mfeldheim/klyra/internal/leader"
	"github.com/mfeldheim/klyra/internal/mcp"
	"github.com/mfeldheim/klyra/internal/server"
	"github.com/mfeldheim/klyra/internal/state"
)

// Version is the klyra build version; overridable at build time via -ldflags.
var Version = "dev"

var (
	flagConfigPath string
	flagAddr       string
	flagNamespace  string
	flagLeaseName  string
	flagKubeconfig string
)

var rootCmd = &cobra.Command{
	Use:   "klyra",
	Short: "Kubernetes monitoring tool",
	RunE:  run,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVar(&flagConfigPath, "config", "/etc/klyra/config.yaml", "path to config file")
	rootCmd.Flags().StringVar(&flagAddr, "addr", ":8080", "HTTP listen address")
	rootCmd.Flags().StringVar(&flagNamespace, "namespace", "default", "Kubernetes namespace")
	rootCmd.Flags().StringVar(&flagLeaseName, "lease-name", "klyra-leader", "leader election lease name")
	rootCmd.Flags().StringVar(&flagKubeconfig, "kubeconfig", "", "path to kubeconfig (empty = in-cluster)")
}

func run(cmd *cobra.Command, args []string) error {
	f, err := os.Open(flagConfigPath)
	if err != nil {
		return err
	}
	defer f.Close()

	cfg, err := config.Load(f)
	if err != nil {
		return err
	}

	k8sClient, err := buildK8sClient(flagKubeconfig)
	if err != nil {
		return err
	}

	st := state.NewStore()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := engine.LoadFromConfigMap(ctx, st, k8sClient, flagNamespace, "klyra-state"); err != nil {
		log.Printf("warning: could not load persisted state: %v", err)
	}

	h := server.NewHandlers(st, cfg)
	h.SetK8sClient(k8sClient)

	// Wire incident system if configured.
	var incMgr *incident.Manager
	var incStore incident.Store
	if cfg.Incidents != nil {
		store, mgr, wireErr := buildIncidentSystem(ctx, cfg)
		if wireErr != nil {
			log.Printf("warning: incident system disabled: %v", wireErr)
		} else {
			incStore = store
			incMgr = mgr
			h.SetIncidentManager(mgr, store)
		}
	}

	// MCP server: read-only Kubernetes + klyra diagnostics for external agents.
	metricsClient := buildMetricsClient(k8sClient)
	k8sTools := diagnostics.NewK8sTools(k8sClient, metricsClient)
	h.SetMCPHandler(mcp.New("klyra", Version, mcp.BuildTools(mcp.Deps{
		K8s:       k8sTools,
		Store:     st,
		Config:    cfg,
		Incidents: incStore,
	})))

	srv := server.New(h, server.UIFileSystem())
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.ListenAndServe(ctx, flagAddr)
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("server: %w", err)
	case <-time.After(100 * time.Millisecond):
	}

	eng, err := engine.New(cfg, st, k8sClient, flagNamespace)
	if err != nil {
		return err
	}

	if incMgr != nil {
		eng.SetIncidentManager(incMgr)
	}

	// isLeader guards the state reader: when this pod is the leader, the engine
	// owns in-memory state and we must not overwrite it with ConfigMap reads.
	var isLeader atomic.Bool

	// State reader: non-leader pods refresh from ConfigMap every 10s so the
	// API serves fresh data without running the engine themselves.
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if !isLeader.Load() {
					if err := engine.LoadFromConfigMap(ctx, st, k8sClient, flagNamespace, "klyra-state"); err != nil {
						log.Printf("state reader: %v", err)
					}
				}
			}
		}
	}()

	leader.Run(ctx, k8sClient, flagNamespace, flagLeaseName,
		func(leaderCtx context.Context) {
			isLeader.Store(true)
			log.Println("leader: acquired lease")

			// Refresh persisted alarm state before reconciling incidents: the
			// state reader stops while we are leader, and the new leader must
			// self-heal incidents orphaned by a restart/failover using the
			// freshest state available.
			if err := engine.LoadFromConfigMap(leaderCtx, st, k8sClient, flagNamespace, "klyra-state"); err != nil {
				log.Printf("leader: reload state: %v", err)
			}
			if incMgr != nil {
				if err := incMgr.Reconcile(leaderCtx, func(name string) (time.Time, bool) {
					alarm, ok := st.GetAlarm(name)
					if !ok || alarm.Status != state.AlarmFiring || alarm.FiredAt == nil {
						return time.Time{}, false
					}
					return *alarm.FiredAt, true
				}); err != nil {
					log.Printf("leader: incident reconcile: %v", err)
				}
			}

			log.Println("leader: starting engine")
			if err := eng.Run(leaderCtx); err != nil {
				log.Printf("engine error: %v", err)
			}
			isLeader.Store(false)
		},
		func() { log.Println("leader: engine stopped") },
	)

	if err := <-serverErr; err != nil {
		log.Printf("server shutdown: %v", err)
	}
	return nil
}

// buildIncidentSystem constructs the S3-backed incident store and manager.
func buildIncidentSystem(ctx context.Context, cfg *config.Config) (incident.Store, *incident.Manager, error) {
	ic := cfg.Incidents

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(ic.S3Region))
	if err != nil {
		return nil, nil, fmt.Errorf("load AWS config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg)
	incStore := incident.NewS3Store(s3Client, ic.S3Bucket, ic.S3Prefix)
	mgr := incident.NewManager(incStore)
	return incStore, mgr, nil
}

// buildMetricsClient tries to build a metrics clientset from the same in-cluster config.
// Returns nil on failure (metrics will show as unavailable in tool responses).
func buildMetricsClient(k8sClient kubernetes.Interface) metricsv.Interface {
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		// Running locally without in-cluster config — metrics unavailable
		return nil
	}
	mc, err := metricsv.NewForConfig(restCfg)
	if err != nil {
		return nil
	}
	return mc
}

func buildK8sClient(kubeconfig string) (kubernetes.Interface, error) {
	var restCfg *rest.Config
	var err error
	if kubeconfig != "" {
		restCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		restCfg, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(restCfg)
}
