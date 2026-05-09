package leader

import (
	"context"
	"log"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

// Run acquires a Kubernetes Lease lock and calls onStartedLeading while the
// pod holds the lease. onStoppedLeading is called when the lease is lost.
// Returns when ctx is cancelled.
func Run(ctx context.Context, client kubernetes.Interface, namespace, leaseName string,
	onStartedLeading func(ctx context.Context),
	onStoppedLeading func(),
) {
	id, _ := os.Hostname()

	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      leaseName,
			Namespace: namespace,
		},
		Client: client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: onStartedLeading,
			OnStoppedLeading: func() {
				log.Println("leader: lost lease")
				onStoppedLeading()
			},
			OnNewLeader: func(identity string) {
				if identity != id {
					log.Printf("leader: current leader is %s", identity)
				}
			},
		},
	})
}
