package controllers

const (
	DaemonSetStr   string = "DaemonSet"
	StatefulSetStr string = "StatefulSet"
	DeploymentStr  string = "Deployment"

	MainContainerAnnotationKey = "vpa-butler.cloud.sap/main-container"
	UpdateModeAnnotationKey    = "vpa-butler.cloud.sap/update-mode"
)
