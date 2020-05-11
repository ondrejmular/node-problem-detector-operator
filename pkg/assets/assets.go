package assets

func ReadAsset(asset string) string {
	return string(MustAsset(asset))
}

const (
	ConfigDockerMonitor        = "assets/configs/journald/docker_monitor.json"
	ConfigKernelMonitor        = "assets/configs/journald/kernel_monitor.json"
	ConfigKubeletMonitor       = "assets/configs/custom/kubelet_monitor.json"
	ConfigFileExistenceMonitor = "assets/configs/custom/file-existence-monitor.json"
)

const (
	PluginKubeletHealth      = "assets/plugins/kubelet-health.sh"
	PluginCheckFileExistence = "assets/plugins/check-file-existence.sh"
)
