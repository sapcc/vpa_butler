package main

import (
	"github.com/sapcc/vpa_butler/internal/controllers"
	"k8s.io/apimachinery/pkg/runtime"
	autoscaling "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"time"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)


func main() {
	_ = autoscaling.AddToScheme(scheme)
	_ = clientgoscheme.AddToScheme(scheme)
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	setupLog.Info("starting")
	syncPeriod := 5*time.Minute
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: ":8080",
		Port:               9443,
		LeaderElection:     false,
		Namespace:          "",
		SyncPeriod:         &syncPeriod,
	})
	handleError(err, "unable to start manager")
	deploymentController := controllers.VPADeploymentController{
		Client: mgr.GetClient(),
		Log: mgr.GetLogger().WithName("deployment-controller"),
		Scheme: mgr.GetScheme(),
		ReSyncPeriod: syncPeriod,
	}
	err = deploymentController.SetupWithManager(mgr)
	handleError(err, "unable to setup deployment controller")
	daemonsetController := controllers.VPADaemonsetController{
		Client:       mgr.GetClient(),
		Log:          mgr.GetLogger().WithName("daemonset-controller"),
		Scheme:       mgr.GetScheme(),
		ReSyncPeriod: syncPeriod,
	}
	err = daemonsetController.SetupWithManager(mgr)
	handleError(err, "unable to setup daemonset controller")
	statefulSetController := controllers.VPAStatefulSetController{
		Client: mgr.GetClient(),
		Log: mgr.GetLogger().WithName("statefulset-controller"),
		Scheme: mgr.GetScheme(),
		ReSyncPeriod: syncPeriod,
	}
	err = statefulSetController.SetupWithManager(mgr)
	handleError(err, "unable to setup statefulset controller")
	setupLog.Info("starting manager")
	err = mgr.Start(ctrl.SetupSignalHandler())
	handleError(err, "problem running manager")
}

func handleError(err error, message string, keysAndVals ...interface{}) {
	if err != nil {
		setupLog.Error(err, message, keysAndVals...)
		os.Exit(1)
	}
}