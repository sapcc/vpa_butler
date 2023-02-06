package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sapcc/vpa_butler/internal/controllers"
	"k8s.io/apimachinery/pkg/runtime"
	autoscaling "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/sapcc/vpa_butler/internal/common"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = autoscaling.AddToScheme(scheme)
	_ = clientgoscheme.AddToScheme(scheme)
}

func main() {
	supportedUpdatedModes := []string{"Off", "Initial", "Recreate"}
	var defaultVPAUpdateMode string
	flag.StringVar(&defaultVPAUpdateMode, "default-vpa-update-mode", "Off",
		fmt.Sprintf("The default update mode for the VPA instances. Must be one of: %s", strings.Join(supportedUpdatedModes, ",")))

	supportedValues := []string{"RequestsOnly", "RequestsAndLimits"}
	var defaultVPASupportedValues string
	flag.StringVar(&defaultVPASupportedValues, "default-vpa-supported-values", "RequestsOnly",
		fmt.Sprintf("Controls which resource value should be autoscaled. Must be one of: %s", strings.Join(supportedValues, ",")))

	flag.Parse()

	// Helm requires the 'Off' value to be quoted to avoid it being interpreted as a boolean.
	defaultVPAUpdateMode = strings.TrimPrefix(defaultVPAUpdateMode, "\"")
	defaultVPAUpdateMode = strings.TrimSuffix(defaultVPAUpdateMode, "\"")
	switch defaultVPAUpdateMode {
	case "Initial":
		common.VPAUpdateMode = autoscaling.UpdateModeInitial
	case "Recreate":
		common.VPAUpdateMode = autoscaling.UpdateModeRecreate
	case "Off":
		common.VPAUpdateMode = autoscaling.UpdateModeOff
	default:
		fmt.Printf("unsupported update mode %s. Must be one of: %s", defaultVPAUpdateMode, strings.Join(supportedUpdatedModes, ","))
		os.Exit(1)
	}

	switch defaultVPASupportedValues {
	case "RequestsAndLimits":
		common.VPAControlledValues = autoscaling.ContainerControlledValuesRequestsAndLimits
	case "RequestsOnly":
		common.VPAControlledValues = autoscaling.ContainerControlledValuesRequestsOnly
	default:
		fmt.Printf("supported values must be one of: %s", strings.Join(supportedValues, ","))
		os.Exit(1)
	}

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	setupLog.Info("starting")
	syncPeriod := 5 * time.Minute
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
	}
	err = deploymentController.SetupWithManager(mgr)
	handleError(err, "unable to setup deployment controller")
	daemonsetController := controllers.VPADaemonsetController{
		Client: mgr.GetClient(),
	}
	err = daemonsetController.SetupWithManager(mgr)
	handleError(err, "unable to setup daemonset controller")
	statefulSetController := controllers.VPAStatefulSetController{
		Client: mgr.GetClient(),
	}
	err = statefulSetController.SetupWithManager(mgr)
	handleError(err, "unable to setup statefulset controller")
	cleanupController := controllers.VPACleanupController{
		Client: mgr.GetClient(),
	}
	err = cleanupController.SetupWithManager(mgr)
	handleError(err, "unable to setup cleanup controller")
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
