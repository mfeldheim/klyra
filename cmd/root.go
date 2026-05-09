// cmd/root.go
package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	_ "github.com/mfeldheim/klyra/internal/action/http"
	_ "github.com/mfeldheim/klyra/internal/monitor/http"
	_ "github.com/mfeldheim/klyra/internal/monitor/kubernetes"
	_ "github.com/mfeldheim/klyra/internal/monitor/prometheus"

	"github.com/mfeldheim/klyra/internal/config"
	"github.com/mfeldheim/klyra/internal/engine"
	"github.com/mfeldheim/klyra/internal/leader"
	"github.com/mfeldheim/klyra/internal/server"
	"github.com/mfeldheim/klyra/internal/state"
)

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
	srv := server.New(h, nil) // UI fs injected at build time via embed.go
	go func() {
		if err := srv.ListenAndServe(flagAddr); err != nil {
			log.Printf("server error: %v", err)
		}
	}()

	eng, err := engine.New(cfg, st, k8sClient, flagNamespace)
	if err != nil {
		return err
	}

	leader.Run(ctx, k8sClient, flagNamespace, flagLeaseName,
		func(leaderCtx context.Context) {
			log.Println("leader: starting engine")
			if err := eng.Run(leaderCtx); err != nil {
				log.Printf("engine error: %v", err)
			}
		},
		func() { log.Println("leader: engine stopped") },
	)

	return nil
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
