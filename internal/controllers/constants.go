package controllers

const (
	DaemonSetStr   string = "DaemonSet"
	StatefulSetStr string = "StatefulSet"
	DeploymentStr  string = "Deployment"

	MainContainerAnnotationKey    string = "vpa-butler.cloud.sap/main-container"
	UpdateModeAnnotationKey       string = "vpa-butler.cloud.sap/update-mode"
	ControlledValuesAnnotationKey string = "vpa-butler.cloud.sap/controlled-values"
)
