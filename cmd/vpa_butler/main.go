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

const (
	webhookPort = 9443
)

var (
	scheme     = runtime.NewScheme()
	setupLog   = ctrl.Log.WithName("setup")
	syncPeriod = 5 * time.Minute

	Version string
)

func init() {
	_ = autoscaling.AddToScheme(scheme)
	_ = clientgoscheme.AddToScheme(scheme)
}

func main() {
	supportedUpdatedModes := []string{"Off", "Initial", "Recreate"}
	var defaultVpaUpdateMode string
	flag.StringVar(&defaultVpaUpdateMode, "default-vpa-update-mode", "Off",
		fmt.Sprintf("The default update mode for the vpa instances. Must be one of: %s",
			strings.Join(supportedUpdatedModes, ",")))

	supportedValues := []string{"RequestsOnly", "RequestsAndLimits"}
	var defaultVpaSupportedValues string
	flag.StringVar(&defaultVpaSupportedValues, "default-vpa-supported-values", "RequestsOnly",
		fmt.Sprintf("Controls which resource value should be autoscaled. Must be one of: %s",
			strings.Join(supportedValues, ",")))
	flag.Parse()

	// Helm requires the 'Off' value to be quoted to avoid it being interpreted as a boolean.
	defaultVpaUpdateMode = strings.Trim(defaultVpaUpdateMode, "\"")
	switch defaultVpaUpdateMode {
	case "Initial":
		common.VpaUpdateMode = autoscaling.UpdateModeInitial
	case "Recreate":
		common.VpaUpdateMode = autoscaling.UpdateModeRecreate
	case "Off":
		common.VpaUpdateMode = autoscaling.UpdateModeOff
	default:
		fmt.Printf("unsupported update mode %s. Must be one of: %s",
			defaultVpaUpdateMode,
			strings.Join(supportedUpdatedModes, ","))
		os.Exit(1)
	}

	switch defaultVpaSupportedValues {
	case "RequestsAndLimits":
		common.VpaControlledValues = autoscaling.ContainerControlledValuesRequestsAndLimits
	case "RequestsOnly":
		common.VpaControlledValues = autoscaling.ContainerControlledValuesRequestsOnly
	default:
		fmt.Printf("supported values must be one of: %s", strings.Join(supportedValues, ","))
		os.Exit(1)
	}

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	setupLog.Info("starting")
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: ":8080",
		Port:               webhookPort,
		LeaderElection:     false,
		Namespace:          "",
		SyncPeriod:         &syncPeriod,
	})

	handleError(err, "unable to start manager")
	handleError(controllers.SetupForAppsV1(mgr), "unable to setup apps/v1 controllers")
	vpaController := controllers.VpaController{
		Client:  mgr.GetClient(),
		Version: Version,
	}
	handleError(vpaController.SetupWithManager(mgr), "unable to setup vpa controller")
	setupLog.Info("starting manager")
	handleError(mgr.Start(ctrl.SetupSignalHandler()), "problem running manager")
}

func handleError(err error, message string) {
	if err != nil {
		setupLog.Error(err, message)
		os.Exit(1)
	}
}
